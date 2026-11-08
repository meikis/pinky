import groovy.json.JsonSlurper
import java.io.File

plugins {
    id("com.android.application")
}

// versionName is Pinky's version (cmd/pinky/mygo.json) and versionCode the
// number of commits up to the one built, so a build of a newer commit
// installs over an older one and the Go driver can compare the installed
// app with the APK Pinky bundles. -Ppinky.versionCode=N overrides the count.
val companionVersionName: String = providers
    .fileContents(rootProject.layout.projectDirectory.file("../../cmd/pinky/mygo.json"))
    .asText
    .map { (JsonSlurper().parseText(it) as Map<*, *>)["version"] as String }
    .get()

fun git(vararg args: String): String? = try {
    val out = providers.exec {
        commandLine("git", *args)
        isIgnoreExitValue = true
    }
    if (out.result.get().exitValue == 0) out.standardOutput.asText.get().trim() else null
} catch (_: Exception) {
    null
}

val companionVersionCode: Int = providers.gradleProperty("pinky.versionCode").orNull?.toInt() ?: run {
    if (git("rev-parse", "--is-shallow-repository") == "true") {
        throw GradleException(
            "android/companion: versionCode is the commit count, but this clone is shallow. " +
                "Fetch the whole history (git fetch --unshallow, or fetch-depth: 0 for actions/checkout) " +
                "or pass -Ppinky.versionCode=N.",
        )
    }
    git("rev-list", "--count", "HEAD")?.toIntOrNull()
        ?: throw GradleException("android/companion: cannot count commits with git; pass -Ppinky.versionCode=N.")
}

// Release builds are signed with Pinky's private release key, which only
// the release workflow has (GitHub secrets, see docs/RELEASING.md): Android
// installs an update only when it is signed with the installed app's key,
// so nobody else can sign an update of a released Companion. The workflow
// passes the keystore's path and password in these variables; a release
// build without them stops. Builds until 2026-11-08 were all signed with
// a key that was committed here, and so is public: Pinky replaces them.
val releaseKeystore: Provider<String> = providers.environmentVariable("PINKY_COMPANION_KEYSTORE")
val releaseKeystorePassword: Provider<String> = providers.environmentVariable("PINKY_COMPANION_KEYSTORE_PASSWORD")
val releaseKeyAlias: Provider<String> = providers.environmentVariable("PINKY_COMPANION_KEY_ALIAS").orElse("pinky-companion")

android {
    namespace = "app.pinky.companion"
    compileSdk {
        version = release(36) {
            minorApiLevel = 1
        }
    }

    defaultConfig {
        applicationId = "app.pinky.companion"
        minSdk = 29
        targetSdk = 36
        versionCode = companionVersionCode
        versionName = companionVersionName
        // The abstract socket the service listens on; Pinky's Go client
        // knows both builds' (internal/driver/android/companion).
        buildConfigField("String", "SOCKET_NAME", "\"pinky_companion\"")
    }

    signingConfigs {
        create("release") {
            if (releaseKeystore.isPresent) {
                storeFile = file(releaseKeystore.get())
                storePassword = releaseKeystorePassword.orNull
                keyAlias = releaseKeyAlias.get()
                // PKCS12 keystores keep the key under the store's password.
                keyPassword = releaseKeystorePassword.orNull
            }
        }
    }

    buildTypes {
        debug {
            // Builds from the source (make companion, which make app
            // bundles): an application id of their own, so that they never
            // install as an update of a released Companion and can sit
            // beside one, signed with the machine's own debug key
            // (~/.android/debug.keystore), which Android Studio's tools
            // create.
            applicationIdSuffix = ".dev"
            buildConfigField("String", "SOCKET_NAME", "\"pinky_companion_dev\"")
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            signingConfig = signingConfigs.getByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        buildConfig = true
    }
}

// A release build without the release key would come out unsigned, or be
// signed with something else by mistake: stop instead.
tasks.configureEach {
    if (name == "preReleaseBuild") {
        val keystore = releaseKeystore
        val password = releaseKeystorePassword
        doFirst {
            if (!keystore.isPresent || !password.isPresent) {
                throw GradleException(
                    "android/companion: a release build is signed with Pinky's release key; set " +
                        "PINKY_COMPANION_KEYSTORE and PINKY_COMPANION_KEYSTORE_PASSWORD (the release workflow does). " +
                        "Builds from the source are debug builds: ./gradlew assembleDebug (make companion).",
                )
            }
            if (!File(keystore.get()).isFile) {
                throw GradleException("android/companion: PINKY_COMPANION_KEYSTORE names no file")
            }
        }
    }
}

/**
 * Writes assets/pinky-companion.json with the version, so that Pinky can
 * read the version of an APK it bundles without the Android SDK: the APK
 * is a zip file.
 */
abstract class CompanionVersionAsset : DefaultTask() {
    @get:Input
    abstract val applicationId: Property<String>

    @get:Input
    abstract val versionName: Property<String>

    @get:Input
    abstract val versionCode: Property<Int>

    @get:OutputDirectory
    abstract val outputDir: DirectoryProperty

    @TaskAction
    fun write() {
        val dir = outputDir.get().asFile
        dir.mkdirs()
        fun quote(v: String) = "\"" + v.replace("\\", "\\\\").replace("\"", "\\\"") + "\""
        dir.resolve("pinky-companion.json").writeText(
            "{\"package\":${quote(applicationId.get())},\"version_name\":${quote(versionName.get())}," +
                "\"version_code\":${versionCode.get()}}\n",
        )
    }
}

// One per variant: the debug build's package is app.pinky.companion.dev.
androidComponents {
    onVariants { variant ->
        val asset = tasks.register<CompanionVersionAsset>("${variant.name}CompanionVersionAsset") {
            applicationId.set(variant.applicationId)
            versionName.set(companionVersionName)
            versionCode.set(companionVersionCode)
            outputDir.set(layout.buildDirectory.dir("generated/pinky/${variant.name}/assets"))
        }
        variant.sources.assets?.addGeneratedSourceDirectory(asset, CompanionVersionAsset::outputDir)
    }
}
