import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter/services.dart';

export 'app_build.dart';

const _appVersionDefine = String.fromEnvironment('APP_VERSION');

const appVersion = _appVersionDefine == '' ? '0.2.16' : _appVersionDefine;

class AppDevice {
  const AppDevice({this.version = appVersion, this.build = 0, this.abi = ''});
  final String version;
  final int build;
  final String abi;
  static const channel = MethodChannel('duanju/device');

  static Future<AppDevice> detect() async {
    if (defaultTargetPlatform != TargetPlatform.android) {
      return const AppDevice();
    }
    try {
      final data = await channel.invokeMapMethod<String, dynamic>('deviceInfo');
      return AppDevice(
        version: data?['version'] as String? ?? appVersion,
        build: (data?['build'] as num?)?.toInt() ?? 0,
        abi: data?['abi'] as String? ?? '',
      );
    } on PlatformException {
      return const AppDevice();
    } on MissingPluginException {
      return const AppDevice();
    }
  }
}

class AppLayout extends InheritedWidget {
  const AppLayout({super.key, this.version = appVersion, required super.child});
  final String version;

  static String versionOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.version ??
      appVersion;

  @override
  bool updateShouldNotify(AppLayout oldWidget) => version != oldWidget.version;
}
