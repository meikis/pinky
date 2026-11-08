package app.pinky.companion

import android.net.LocalServerSocket
import android.net.LocalSocket
import android.os.Process
import android.os.SystemClock
import android.system.Os
import android.system.OsConstants
import android.util.Log
import java.io.BufferedOutputStream
import java.io.BufferedReader
import java.io.Closeable
import java.io.IOException
import java.io.InputStreamReader
import java.util.concurrent.CopyOnWriteArraySet

/**
 * Serves the line-delimited JSON protocol on the abstract Unix socket
 * `@pinky_companion` (`@pinky_companion_dev` in the development build,
 * which may be installed beside a release). The computer reaches it with
 * `adb forward tcp:0 localabstract:pinky_companion`.
 *
 * Any app could connect to an abstract socket, so only the shell user
 * (adbd), root and this app itself are accepted.
 */
class CompanionServer(private val service: PinkyAccessibilityService) {

    val handler = RequestHandler(service) { connections.size }
    val events: EventHub get() = service.events
    private val connections = CopyOnWriteArraySet<Connection>()

    @Volatile
    private var running = false

    @Volatile
    private var socket: LocalServerSocket? = null

    val connectionCount: Int get() = connections.size

    fun start() {
        running = true
        Thread(::acceptLoop, "pinky-accept").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running = false
        socket?.let { s ->
            // close() alone does not wake a thread blocked in accept() on
            // every Android version; shutting the socket down does.
            try {
                Os.shutdown(s.fileDescriptor, OsConstants.SHUT_RDWR)
            } catch (_: Exception) {
            }
            closeQuietly(s)
        }
        socket = null
        connections.forEach { it.close() }
        handler.shutdown()
    }

    fun touch() = IdleClock.touch()

    internal fun removed(c: Connection) {
        if (connections.remove(c)) touch()
    }

    private fun acceptLoop() {
        val server = bind() ?: return
        socket = server
        if (!running) {
            closeQuietly(server)
            return
        }
        Log.i(TAG, "listening on @$SOCKET_NAME")
        try {
            while (running) {
                val s = try {
                    server.accept()
                } catch (e: IOException) {
                    if (running) Log.w(TAG, "accept failed", e)
                    break
                }
                if (!running) {
                    closeQuietly(s)
                    break
                }
                accept(s)
            }
        } finally {
            closeQuietly(server)
            Log.i(TAG, "socket closed")
        }
    }

    private fun bind(): LocalServerSocket? {
        // A previous instance may still hold the name for a moment after a
        // quick off/on cycle.
        for (attempt in 1..50) {
            if (!running) return null
            try {
                return LocalServerSocket(SOCKET_NAME)
            } catch (e: IOException) {
                if (attempt == 1) Log.w(TAG, "bind @$SOCKET_NAME failed, retrying: $e")
                Thread.sleep(100)
            }
        }
        Log.e(TAG, "could not bind @$SOCKET_NAME")
        return null
    }

    private fun accept(s: LocalSocket) {
        val uid = try {
            s.peerCredentials.uid
        } catch (_: IOException) {
            -1
        }
        if (uid != SHELL_UID && uid != ROOT_UID && uid != Process.myUid()) {
            Log.w(TAG, "rejected a connection from uid $uid")
            try {
                s.outputStream.write(FORBIDDEN)
            } catch (_: IOException) {
            }
            closeQuietly(s)
            return
        }
        val c = Connection(s, this)
        connections.add(c)
        touch()
        Thread(c::run, "pinky-conn").apply {
            isDaemon = true
            start()
        }
    }

    companion object {
        private const val TAG = "PinkyServer"
        val SOCKET_NAME: String = BuildConfig.SOCKET_NAME

        // android.os.Process.SHELL_UID and ROOT_UID are hidden APIs.
        private const val SHELL_UID = 2000
        private const val ROOT_UID = 0

        private val FORBIDDEN =
            "{\"ok\":false,\"error\":{\"code\":\"forbidden\",\"message\":\"only adb may connect\"}}\n".toByteArray()

        fun closeQuietly(c: Closeable?) {
            try {
                c?.close()
            } catch (_: IOException) {
            }
        }
    }
}

/** One client connection. Requests run one at a time; events may interleave. */
class Connection(private val socket: LocalSocket, private val server: CompanionServer) {
    private val out = BufferedOutputStream(socket.outputStream, 64 * 1024)
    private val writeLock = Any()

    @Volatile
    private var closed = false

    @Volatile
    private var subscriber: EventHub.Subscriber? = null

    fun run() {
        try {
            val reader = BufferedReader(InputStreamReader(socket.inputStream, Charsets.UTF_8), 64 * 1024)
            while (!closed) {
                val line = reader.readLine() ?: break
                if (line.isBlank()) continue
                server.touch()
                val reply = server.handler.handle(line, this)
                send(reply.line)
                reply.after?.invoke()
            }
        } catch (e: IOException) {
            if (!closed) Log.d("PinkyConn", "connection ended: $e")
        } finally {
            close()
        }
    }

    /**
     * Replaces this connection's event subscription; an empty set
     * unsubscribes. A display limits it to that display's events.
     */
    fun subscribe(kinds: Set<String>, display: Int?): EventHub.Subscriber? {
        val hub = server.events
        subscriber?.let { hub.unsubscribe(it) }
        subscriber = if (kinds.isEmpty()) null else hub.subscribe(this, kinds, display)
        return subscriber
    }

    @Throws(IOException::class)
    fun send(line: String) {
        val bytes = line.toByteArray(Charsets.UTF_8)
        synchronized(writeLock) {
            out.write(bytes)
            out.write('\n'.code)
            out.flush()
        }
    }

    fun close() {
        if (closed) return
        closed = true
        subscriber?.let { server.events.unsubscribe(it) }
        subscriber = null
        CompanionServer.closeQuietly(socket)
        server.removed(this)
    }
}

/**
 * When a client last connected, sent a request or went away, for the idle
 * watchdog. It belongs to the process rather than to a service instance:
 * Android unbinds the service and binds it again for every uiautomator
 * dump, and that must not restart the count. Elapsed realtime, so that time
 * the phone spent asleep counts too.
 */
object IdleClock {
    @Volatile
    private var last = SystemClock.elapsedRealtime()

    fun touch() {
        last = SystemClock.elapsedRealtime()
    }

    fun idleMs(): Long = SystemClock.elapsedRealtime() - last
}
