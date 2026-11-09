package com.duanju.duanju_app

import io.flutter.embedding.android.FlutterActivity
import android.app.UiModeManager
import android.content.Intent
import android.os.Build
import android.provider.Settings
import android.os.Bundle
import android.content.Context
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.media.AudioManager
import android.view.WindowManager
import android.net.ConnectivityManager
import android.net.Uri
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import androidx.core.content.FileProvider
import com.duanju.duanju_app.playback.ExoPlaybackHost
import java.io.File

class MainActivity : FlutterActivity() {
    private var playback: ExoPlaybackHost? = null

    private fun installApk(path: String): Boolean {
        if (path.isEmpty()) return false
        val file = File(path)
        if (!file.isFile) return false
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !packageManager.canRequestPackageInstalls()) {
            try {
                startActivity(
                    Intent(
                        Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                        Uri.parse("package:$packageName"),
                    ),
                )
            } catch (_: Throwable) {
            }
            return false
        }
        return try {
            val uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", file)
            val intent = Intent(Intent.ACTION_VIEW).apply {
                setDataAndType(uri, "application/vnd.android.package-archive")
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            startActivity(intent)
            true
        } catch (_: Throwable) {
            false
        }
    }

    private fun brightness(): Double {
        val current = window.attributes.screenBrightness
        if (current >= 0f) return current.toDouble()
        val system = try {
            Settings.System.getInt(contentResolver, Settings.System.SCREEN_BRIGHTNESS)
        } catch (_: Throwable) {
            128
        }
        return (system / 255.0).coerceIn(0.0, 1.0)
    }

    private fun setBrightness(value: Float) {
        window.attributes = window.attributes.apply { screenBrightness = value }
    }

    private fun mediaVolume(): Double {
        val audio = getSystemService(Context.AUDIO_SERVICE) as AudioManager
        val max = audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC)
        if (max <= 0) return 0.0
        return audio.getStreamVolume(AudioManager.STREAM_MUSIC).toDouble() / max
    }

    private fun setMediaVolume(value: Double) {
        val audio = getSystemService(Context.AUDIO_SERVICE) as AudioManager
        val max = audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC)
        val target = Math.round(value.coerceIn(0.0, 1.0) * max).toInt()
        if (target != audio.getStreamVolume(AudioManager.STREAM_MUSIC)) {
            try {
                audio.setStreamVolume(AudioManager.STREAM_MUSIC, target, 0)
            } catch (_: Throwable) {
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            window.isNavigationBarContrastEnforced = false
            window.isStatusBarContrastEnforced = false
        }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        playback = ExoPlaybackHost(flutterEngine, applicationContext)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "duanju/update")
            .setMethodCallHandler { call, result ->
                if (call.method == "installApk") {
                    result.success(installApk(call.argument<String>("path").orEmpty()))
                } else {
                    result.notImplemented()
                }
            }
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "duanju/device")
            .setMethodCallHandler { call, result ->
                if (call.method == "deviceInfo") {
                    val mode = getSystemService(Context.UI_MODE_SERVICE) as UiModeManager
                    val television = mode.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION ||
                        packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
                    val info = packageManager.getPackageInfo(packageName, 0)
                    val version = info.versionName
                    val build = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                        info.longVersionCode
                    } else {
                        @Suppress("DEPRECATION")
                        info.versionCode.toLong()
                    }
                    result.success(
                        mapOf(
                            "television" to television,
                            "version" to version,
                            "build" to build,
                            "abi" to Build.SUPPORTED_ABIS.firstOrNull().orEmpty(),
                        ),
                    )
                } else if (call.method == "brightness") {
                    result.success(brightness())
                } else if (call.method == "setBrightness") {
                    val value = call.argument<Double>("value") ?: brightness()
                    setBrightness(value.toFloat().coerceIn(0.01f, 1f))
                    result.success(null)
                } else if (call.method == "resetBrightness") {
                    setBrightness(WindowManager.LayoutParams.BRIGHTNESS_OVERRIDE_NONE)
                    result.success(null)
                } else if (call.method == "mediaVolume") {
                    result.success(mediaVolume())
                } else if (call.method == "setMediaVolume") {
                    setMediaVolume(call.argument<Double>("value") ?: mediaVolume())
                    result.success(null)
                } else if (call.method == "systemProxy") {
                    val connection = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
                    val proxy = connection.defaultProxy
                    val host = proxy?.host.orEmpty()
                    val address = if (host.isNotEmpty() && (proxy?.port ?: 0) > 0) {
                        "http://${if (host.contains(':')) "[$host]" else host}:${proxy!!.port}"
                    } else ""
                    result.success(mapOf("http" to address, "https" to address,
                        "bypass" to (proxy?.exclusionList?.toList() ?: emptyList<String>()),
                        "pac" to (proxy != null && proxy.pacFileUrl != Uri.EMPTY)))
                } else {
                    result.notImplemented()
                }
            }
    }
}
