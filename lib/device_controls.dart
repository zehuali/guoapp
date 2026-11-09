import 'package:flutter/services.dart';

class DeviceControls {
  static const _channel = MethodChannel('duanju/device');

  static Future<double?> _read(String method) async {
    try {
      return (await _channel.invokeMethod<num>(method))?.toDouble();
    } catch (_) {
      return null;
    }
  }

  static Future<void> _write(String method, [double? value]) async {
    try {
      await _channel.invokeMethod<void>(method, {'value': ?value});
    } catch (_) {}
  }

  static Future<double?> brightness() => _read('brightness');

  static Future<void> setBrightness(double value) =>
      _write('setBrightness', value.clamp(0.0, 1.0));

  static Future<void> resetBrightness() => _write('resetBrightness');

  static Future<double?> mediaVolume() => _read('mediaVolume');

  static Future<void> setMediaVolume(double value) =>
      _write('setMediaVolume', value.clamp(0.0, 1.0));
}
