import 'package:media_kit/media_kit.dart';

import 'playback_media_kit.dart';
import 'playback_player.dart';

PlaybackPlayer createPlaybackPlayer() => MediaKitPlaybackPlayer(
  Player(
    configuration: const PlayerConfiguration(bufferSize: 32 * 1024 * 1024),
  ),
);
