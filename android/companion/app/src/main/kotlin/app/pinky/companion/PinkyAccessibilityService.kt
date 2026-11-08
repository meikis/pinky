package app.pinky.companion

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.annotation.SuppressLint
import android.content.ComponentName
import android.content.Intent
import android.content.SharedPreferences
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import org.json.JSONObject

/**
 * The accessibility service Pinky turns on over adb only while an agent
 * works on the phone, and off again afterwards: the phone's apps can see an
 * accessibility service that is on, and banking apps refuse to sign in next
 * to one that is not from an app store. While it is connected it serves the
 * companion protocol (see docs/notes/companion-protocol.md); it never
 * changes what the phone does.
 */
class PinkyAccessibilityService : AccessibilityService() {

    val labels = AppLabels(this)
    val displays = Displays(this)
    val events = EventHub(labels, displays)

    lateinit var tree: TreeDumper
        private set

    lateinit var textSetter: TextSetter
        private set

    /** Turn off after this long without a connection; 0 keeps the service on. */
    @Volatile
    var idleDisableMs = DEFAULT_IDLE_DISABLE_MS
        private set

    @Volatile
    var includeNotImportant = false
        private set

    private var server: CompanionServer? = null
    private val main = Handler(Looper.getMainLooper())

    // What config set, kept until the service is turned off, so that it
    // survives the service restarting: a reboot, an app update, a
    // uiautomator dump. Pinky until 2026-11-08 kept the service on for
    // good this way (idle_disable_ms 0).
    private val prefs: SharedPreferences by lazy { getSharedPreferences(PREFS, MODE_PRIVATE) }

    override fun onCreate() {
        super.onCreate()
        tree = TreeDumper(this)
        textSetter = TextSetter(this)
    }

    override fun onServiceConnected() {
        super.onServiceConnected()
        idleDisableMs = prefs.getLong(KEY_IDLE_DISABLE_MS, DEFAULT_IDLE_DISABLE_MS)
        includeNotImportant =
            serviceInfo.flags and AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS != 0
        val keep = prefs.getBoolean(KEY_INCLUDE_NOT_IMPORTANT, includeNotImportant)
        if (keep != includeNotImportant) setIncludeNotImportant(keep)
        // A service that was only restarted keeps counting idle time from
        // its clients' last activity; one that was turned on starts now.
        if (!restarted) IdleClock.touch()
        restarted = false
        server?.stop()
        server = CompanionServer(this).also { it.start() }
        // Which display each open app is on, for toasts that come before
        // any window event of their app.
        server?.handler?.runOnWorker {
            val seed = displays.appDisplays()
            main.post { events.seed(seed) }
        }
        // Check now rather than 10 s from now, so that frequent rebinding
        // cannot postpone the check for ever.
        main.removeCallbacks(watchdog)
        main.post(watchdog)
        Log.i(TAG, "connected")
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        if (event != null) events.onEvent(event)
    }

    override fun onInterrupt() {}

    // commit, not apply: the process may be killed right after an unbind.
    @SuppressLint("ApplySharedPref")
    override fun onUnbind(intent: Intent?): Boolean {
        shutdown()
        // Android also unbinds the service without turning it off, for a
        // uiautomator dump or an update; only a service that was turned off
        // (in Android Settings, by Pinky or by the watchdog) forgets config.
        val listed = listedInSettings()
        if (!listed) prefs.edit().clear().commit()
        restarted = listed
        return super.onUnbind(intent)
    }

    override fun onDestroy() {
        shutdown()
        labels.shutdown()
        super.onDestroy()
    }

    /**
     * Applies a config request and returns the resulting settings. They are
     * saved until the service is turned off; committed before the reply, so
     * an app update right after it cannot lose them.
     */
    @SuppressLint("ApplySharedPref")
    fun configure(req: JSONObject): String {
        val edit = prefs.edit()
        if (req.has("include_not_important")) {
            val v = req.getBoolean("include_not_important")
            setIncludeNotImportant(v)
            edit.putBoolean(KEY_INCLUDE_NOT_IMPORTANT, v)
        }
        if (req.has("idle_disable_ms")) {
            idleDisableMs = req.getLong("idle_disable_ms").coerceAtLeast(0)
            edit.putLong(KEY_IDLE_DISABLE_MS, idleDisableMs)
        }
        edit.commit()
        val j = Json()
        j.beginObject()
        j.field("include_not_important", includeNotImportant)
        j.field("idle_disable_ms", idleDisableMs)
        j.endObject()
        return j.toString()
    }

    private fun setIncludeNotImportant(v: Boolean) {
        val info = serviceInfo
        val flag = AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS
        info.flags = if (v) info.flags or flag else info.flags and flag.inv()
        serviceInfo = info
        includeNotImportant = v
        // Cached nodes were fetched with the old flag.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) clearCache()
    }

    /**
     * Whether enabled_accessibility_services still lists this service. True
     * when the setting cannot be read, so that config is kept rather than
     * lost.
     */
    private fun listedInSettings(): Boolean = try {
        val me = ComponentName(this, PinkyAccessibilityService::class.java)
        val value = Settings.Secure.getString(contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES)
        value.orEmpty().split(':').any { ComponentName.unflattenFromString(it.trim()) == me }
    } catch (e: SecurityException) {
        Log.w(TAG, "cannot read the enabled services", e)
        true
    }

    private fun shutdown() {
        main.removeCallbacks(watchdog)
        server?.stop()
        server = null
        events.closeAll()
    }

    // A backstop for a Pinky that crashed or lost the phone without turning
    // the service off: no client for idleDisableMs turns it off. While an
    // agent works, Pinky keeps an event subscription open.
    private val watchdog = object : Runnable {
        override fun run() {
            val s = server ?: return
            val limit = idleDisableMs
            val idle = IdleClock.idleMs()
            if (limit > 0 && s.connectionCount == 0 && idle >= limit) {
                Log.i(TAG, "no client for ${idle / 1000} s (limit ${limit / 1000} s), turning the service off")
                disableSelf()
                return
            }
            main.postDelayed(this, WATCHDOG_INTERVAL_MS)
        }
    }

    companion object {
        private const val TAG = "PinkyService"
        const val DEFAULT_IDLE_DISABLE_MS = 60_000L

        // Whether the last unbind left the service turned on (a restart),
        // across service instances in this process. Main thread only.
        private var restarted = false
        private const val WATCHDOG_INTERVAL_MS = 10_000L
        private const val PREFS = "service_config"
        private const val KEY_IDLE_DISABLE_MS = "idle_disable_ms"
        private const val KEY_INCLUDE_NOT_IMPORTANT = "include_not_important"
    }
}
