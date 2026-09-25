"""Mix the narration clips onto the rendered video.

Each clip from make_narration.ps1 (line00.wav, line01.wav, ...) is delayed to its "start" time in
narration.json, the clips are mixed into one track, and that track is muxed into the MP4 as AAC.
The video stream is copied untouched.

    powershell -File docs/video/make_narration.ps1 -OutDir <clips>
    videnv/Scripts/python docs/video/add_narration.py <clips> [video.mp4] [narration.json]
"""
import json
import pathlib
import subprocess
import sys

import imageio_ffmpeg

HERE = pathlib.Path(__file__).resolve().parent


def video_duration(ffmpeg: str, video: pathlib.Path) -> float:
    """Seconds, parsed from ffmpeg's "Duration: HH:MM:SS.ss" banner line."""
    info = subprocess.run([ffmpeg, "-i", str(video)], capture_output=True, text=True).stderr
    h, m, s = info.split("Duration: ")[1].split(",")[0].split(":")
    return int(h) * 3600 + int(m) * 60 + float(s)


def main() -> None:
    clips = pathlib.Path(sys.argv[1])
    VIDEO = HERE / (sys.argv[2] if len(sys.argv) > 2 else "MTSAi-Data-Lake-Pipeline.mp4")
    narration = sys.argv[3] if len(sys.argv) > 3 else "narration.json"
    lines = json.loads((HERE / narration).read_text(encoding="utf-8"))
    ffmpeg = imageio_ffmpeg.get_ffmpeg_exe()

    cmd = [ffmpeg, "-y", "-loglevel", "error", "-i", str(VIDEO)]
    filters, labels = [], []
    for i, line in enumerate(lines):
        cmd += ["-i", str(clips / f"line{i:02d}.wav")]
        ms = int(line["start"] * 1000)
        filters.append(f"[{i + 1}:a]aresample=48000,adelay={ms}|{ms}[a{i}]")
        labels.append(f"[a{i}]")
    # normalize=0 keeps each voice clip at full volume (the clips never overlap). Pad to exactly
    # the video's length: an unbounded apad + -shortest with a copied video stream hangs ffmpeg.
    duration = video_duration(ffmpeg, VIDEO)
    filters.append(f"{''.join(labels)}amix=inputs={len(lines)}:normalize=0,"
                   f"apad=whole_dur={duration},atrim=0:{duration}[aout]")

    partial = VIDEO.with_suffix(".partial.mp4")
    cmd += ["-filter_complex", ";".join(filters), "-map", "0:v", "-map", "[aout]",
            "-c:v", "copy", "-c:a", "aac", "-b:a", "160k", "-ac", "2",
            "-movflags", "+faststart", str(partial)]
    subprocess.run(cmd, check=True)
    partial.replace(VIDEO)
    print("wrote", VIDEO)


if __name__ == "__main__":
    main()
