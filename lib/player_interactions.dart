import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'device_controls.dart';
import 'player_keys.dart';
import 'widgets.dart';
import 'playback_player.dart';

enum PlayerGestureKind { seek, volume, brightness }

class PlayerGesture {
  const PlayerGesture(
    this.kind,
    this.value, {
    this.origin = 0,
    this.duration = 0,
  });

  final PlayerGestureKind kind;
  final double value;
  final double origin;
  final double duration;
}

class PlayerInteractions extends ChangeNotifier {
  PlayerInteractions({
    required this.player,
    required this.available,
    required this.baseSpeed,
    required this.onTogglePlayback,
    required this.onFullscreen,
    required this.onEpisode,
    required this.keyMap,
    required this.onSettings,
    this.onSeek,
  }) {
    _playing = player.stream.playing.listen((playing) {
      if (!playing) cancel();
    });
  }

  final PlaybackPlayer player;
  final bool Function() available;
  final double Function() baseSpeed;
  final VoidCallback onTogglePlayback;
  final VoidCallback onFullscreen;
  final String Function(int direction) onEpisode;
  final PlayerKeyMap Function() keyMap;
  final VoidCallback onSettings;
  final Future<void> Function(Duration)? onSeek;
  late final StreamSubscription<bool> _playing;
  Timer? _holdTimer;
  Timer? _hintTimer;
  Future<void> _rates = Future<void>.value();
  final Set<int> _pointers = {};
  int? _pointer;
  Offset? _origin;
  Size _surface = Size.zero;
  bool _swipeEnabled = false;
  PlayerGestureKind? _panKind;
  bool _panLocked = false;
  double _panStart = 0;
  PlayerGesture? _gesture;
  double _brightness = .5;
  double _mediaVolume = .5;
  bool _brightnessChanged = false;
  bool _moved = false;
  bool _held = false;
  bool _boosting = false;
  bool _keyboardHold = false;
  bool _cancelUntilRelease = false;
  bool _disposed = false;
  double _unmutedVolume = 100;
  String _feedback = '';
  DateTime _ignoreTapUntil = DateTime(2000);

  String get feedback => _feedback;
  PlayerGesture? get gesture => _gesture;
  bool get boosting => _boosting;
  bool get suppressTap => DateTime.now().isBefore(_ignoreTapUntil);
  Future<void> get pendingRates => _rates;

  void hint(String message, {bool persistent = false}) {
    if (_disposed) return;
    _hintTimer?.cancel();
    if (_feedback != message) {
      _feedback = message;
      notifyListeners();
    }
    if (!persistent && message.isNotEmpty) {
      _hintTimer = Timer(const Duration(milliseconds: 1200), () {
        hint(_boosting ? '3 倍速 · 松开恢复' : '', persistent: true);
      });
    }
  }

  Future<void> applySpeed() => _setRate(baseSpeed());

  Future<void> _setRate(double value) {
    _rates = _rates
        .catchError((Object _) {})
        .then((_) => player.setRate(value));
    unawaited(
      _rates.catchError((Object _) {
        hint('倍速调整失败，请重试');
      }),
    );
    return _rates;
  }

  void _beginHold({bool keyboard = false}) {
    if (!available() || _holdTimer != null || _boosting) return;
    _keyboardHold = keyboard;
    _holdTimer = Timer(const Duration(milliseconds: 350), () {
      _holdTimer = null;
      if (_disposed ||
          !available() ||
          !player.state.playing ||
          player.state.completed) {
        return;
      }
      _boosting = true;
      _held = true;
      unawaited(_setRate(3));
      hint('3 倍速 · 松开恢复', persistent: true);
    });
  }

  void _endHold({bool tap = false, bool silent = false}) {
    final wasKeyboard = _keyboardHold;
    final boosted = _boosting;
    _holdTimer?.cancel();
    _holdTimer = null;
    _keyboardHold = false;
    _boosting = false;
    if (boosted) {
      unawaited(_setRate(baseSpeed()));
      if (!silent) hint('恢复 ${baseSpeed()} 倍速');
    } else if (tap && wasKeyboard) {
      seek(5);
    }
  }

  void cancel() {
    if (_disposed) return;
    if (_pointers.isNotEmpty) {
      _cancelUntilRelease = true;
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    }
    _pointer = null;
    _origin = null;
    _resetPan();
    _endHold(silent: true);
    hint('');
  }

  void _resetPan() {
    _panKind = null;
    _panLocked = false;
    if (_gesture != null) {
      _gesture = null;
      notifyListeners();
    }
  }

  void _refreshDevice() {
    unawaited(
      DeviceControls.brightness().then((value) {
        if (value != null && _panKind != PlayerGestureKind.brightness) {
          _brightness = value;
        }
      }),
    );
    unawaited(
      DeviceControls.mediaVolume().then((value) {
        if (value != null && _panKind != PlayerGestureKind.volume) {
          _mediaVolume = value;
        }
      }),
    );
  }

  void _beginPan(Offset delta) {
    _panLocked = true;
    if (!available()) return;
    if (delta.dx.abs() > delta.dy.abs()) {
      final duration = player.state.duration.inMilliseconds / 1000;
      if (duration <= 0) return;
      _panKind = PlayerGestureKind.seek;
      _panStart = player.state.position.inMilliseconds / 1000;
    } else if (_origin!.dx < _surface.width / 2) {
      _panKind = PlayerGestureKind.brightness;
      _panStart = _brightness;
    } else {
      _panKind = PlayerGestureKind.volume;
      _panStart = _mediaVolume;
    }
    hint('');
    _updatePan(delta);
  }

  void _updatePan(Offset delta) {
    final kind = _panKind;
    if (kind == null || _surface.isEmpty) return;
    if (kind == PlayerGestureKind.seek) {
      final duration = player.state.duration.inMilliseconds / 1000;
      final span = math.min(duration, 120.0);
      final target = (_panStart + delta.dx / _surface.width * span).clamp(
        0.0,
        duration,
      );
      _gesture = PlayerGesture(
        kind,
        target,
        origin: _panStart,
        duration: duration,
      );
    } else {
      final value = (_panStart - delta.dy / (_surface.height * .8)).clamp(
        0.0,
        1.0,
      );
      if (kind == PlayerGestureKind.brightness) {
        _brightness = value;
        _brightnessChanged = true;
        unawaited(DeviceControls.setBrightness(value));
      } else {
        _mediaVolume = value;
        unawaited(DeviceControls.setMediaVolume(value));
      }
      _gesture = PlayerGesture(kind, value);
    }
    notifyListeners();
  }

  void _endPan() {
    final gesture = _gesture;
    if (gesture != null &&
        gesture.kind == PlayerGestureKind.seek &&
        available() &&
        (gesture.value - gesture.origin).abs() >= .5) {
      unawaited(
        (onSeek ?? player.seek)(
          Duration(milliseconds: (gesture.value * 1000).round()),
        ),
      );
    }
    _resetPan();
  }

  void pointerDown(
    PointerDownEvent event, {
    required bool swipeEnabled,
    required Size size,
  }) {
    _pointers.add(event.pointer);
    if (_pointers.length != 1 || _cancelUntilRelease) {
      cancel();
      return;
    }
    if (!available() || event.buttons != kPrimaryButton) return;
    _pointer = event.pointer;
    _origin = event.localPosition;
    _surface = size;
    _swipeEnabled = swipeEnabled && event.kind == PointerDeviceKind.touch;
    _moved = _held = false;
    _resetPan();
    if (_swipeEnabled) _refreshDevice();
    _beginHold();
  }

  void pointerMove(PointerMoveEvent event) {
    if (_pointer != event.pointer || _origin == null || _held) return;
    final delta = event.localPosition - _origin!;
    if (_panKind != null) {
      _updatePan(delta);
      return;
    }
    if (delta.distance > 12 && !_moved) {
      _moved = true;
      _endHold();
      if (_swipeEnabled && !_panLocked) _beginPan(delta);
    }
  }

  void pointerUp(PointerUpEvent event) {
    _pointers.remove(event.pointer);
    if (_cancelUntilRelease) {
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
      if (_pointers.isEmpty) _cancelUntilRelease = false;
      return;
    }
    if (_pointer != event.pointer || _origin == null) return;
    if (_moved || _held) {
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    }
    _pointer = null;
    _origin = null;
    _endPan();
    _endHold();
  }

  void pointerCancel(PointerCancelEvent event) {
    _pointers.remove(event.pointer);
    cancel();
    _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    if (_pointers.isEmpty) _cancelUntilRelease = false;
  }

  void seek(int seconds) {
    if (!available() || player.state.duration <= Duration.zero) return;
    _endHold();
    final target = (player.state.position.inMilliseconds + seconds * 1000)
        .clamp(0, player.state.duration.inMilliseconds);
    unawaited((onSeek ?? player.seek)(Duration(milliseconds: target)));
    hint('${seconds > 0 ? '快进至' : '后退至'} ${formatPosition(target / 1000)}');
  }

  void changeVolume(double delta) {
    if (!available()) return;
    final volume = (player.state.volume + delta).clamp(0.0, 100.0);
    unawaited(player.setVolume(volume));
    if (volume > 0) _unmutedVolume = volume;
    hint(volume == 0 ? '已静音' : '音量 ${volume.round()}%');
  }

  void toggleMute() {
    if (!available()) return;
    final current = player.state.volume;
    if (current > 0) _unmutedVolume = current;
    final target = current > 0 ? 0.0 : _unmutedVolume;
    unawaited(player.setVolume(target));
    hint(target == 0 ? '已静音' : '音量 ${target.round()}%');
  }

  KeyEventResult key(KeyEvent event) {
    final key = event.logicalKey;
    final mapped = PlayerKeyMap.keyOf(key);
    if (event is KeyUpEvent) {
      if (mapped == PlayerKey.right && _keyboardHold) {
        _endHold(tap: available());
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }
    final hardware = HardwareKeyboard.instance;
    if (hardware.isAltPressed ||
        hardware.isMetaPressed ||
        hardware.isShiftPressed) {
      cancel();
      return KeyEventResult.ignored;
    }
    if (key == LogicalKeyboardKey.f11 || key == LogicalKeyboardKey.keyF) {
      if (event is KeyDownEvent) {
        cancel();
        onFullscreen();
      }
      return KeyEventResult.handled;
    }
    if (hardware.isControlPressed || !available()) {
      return KeyEventResult.ignored;
    }
    if (mapped == null) {
      if (key == LogicalKeyboardKey.space ||
          key == LogicalKeyboardKey.mediaPlayPause) {
        if (event is KeyDownEvent) onTogglePlayback();
      } else if (key == LogicalKeyboardKey.keyM) {
        if (event is KeyDownEvent) toggleMute();
      } else if (key == LogicalKeyboardKey.mediaTrackNext ||
          key == LogicalKeyboardKey.mediaTrackPrevious) {
        if (event is KeyDownEvent) {
          hint(onEpisode(key == LogicalKeyboardKey.mediaTrackNext ? 1 : -1));
        }
      } else {
        return KeyEventResult.ignored;
      }
      return KeyEventResult.handled;
    }
    final action = keyMap()[mapped];
    if (action == PlayerKeyAction.none) return KeyEventResult.handled;
    if (action != PlayerKeyAction.seekForward) _endHold();
    if (action == PlayerKeyAction.seekForward) {
      if (event is KeyDownEvent) _beginHold(keyboard: true);
    } else if (action == PlayerKeyAction.seekBackward) {
      seek(-5);
    } else if (action == PlayerKeyAction.volumeUp) {
      changeVolume(5);
    } else if (action == PlayerKeyAction.volumeDown) {
      changeVolume(-5);
    } else if (action == PlayerKeyAction.playPause) {
      if (event is KeyDownEvent) onTogglePlayback();
    } else if (action == PlayerKeyAction.nextEpisode) {
      if (event is KeyDownEvent) hint(onEpisode(1));
    } else if (action == PlayerKeyAction.previousEpisode) {
      if (event is KeyDownEvent) hint(onEpisode(-1));
    } else if (action == PlayerKeyAction.toggleFullscreen) {
      if (event is KeyDownEvent) {
        cancel();
        onFullscreen();
      }
    } else if (action == PlayerKeyAction.toggleMute) {
      if (event is KeyDownEvent) toggleMute();
    } else if (action == PlayerKeyAction.openSettings) {
      if (event is KeyDownEvent) onSettings();
    }
    return KeyEventResult.handled;
  }

  @override
  void dispose() {
    _disposed = true;
    _holdTimer?.cancel();
    _hintTimer?.cancel();
    if (_boosting) unawaited(_setRate(baseSpeed()));
    if (_brightnessChanged) unawaited(DeviceControls.resetBrightness());
    _boosting = false;
    _playing.cancel();
    _pointers.clear();
    super.dispose();
  }
}
