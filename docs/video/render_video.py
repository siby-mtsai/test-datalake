"""Render docs/video/pipeline-video.html to an MP4, frame by frame.

The page exposes window.render(t); this script steps t at a fixed frame rate, screenshots each
frame in the locally installed Microsoft Edge (via Playwright, no browser download), and pipes the
frames into ffmpeg (bundled by imageio-ffmpeg).

    python -m venv videnv
    videnv/Scripts/python -m pip install playwright imageio-ffmpeg
    videnv/Scripts/python docs/video/render_video.py            # pipeline video
    videnv/Scripts/python docs/video/render_video.py --page access-video.html --out MTSAi-Data-Lake-Access.mp4
    videnv/Scripts/python docs/video/render_video.py --stills 10 30 57   # preview PNGs only
"""
import argparse
import pathlib
import subprocess

import imageio_ffmpeg
from playwright.sync_api import sync_playwright

HERE = pathlib.Path(__file__).resolve().parent


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--fps", type=int, default=24)
    ap.add_argument("--stills", type=float, nargs="*")
    ap.add_argument("--page", default="pipeline-video.html")
    ap.add_argument("--out", default="MTSAi-Data-Lake-Pipeline.mp4")
    args = ap.parse_args()
    page_url = (HERE / args.page).as_uri() + "?capture"
    OUT = HERE / args.out

    with sync_playwright() as p:
        browser = p.chromium.launch(channel="msedge")
        page = browser.new_page(viewport={"width": 1920, "height": 1080})
        page.goto(page_url)
        page.wait_for_function("typeof window.render === 'function'")
        stage = page.locator("#stage")

        if args.stills:
            for t in args.stills:
                page.evaluate(f"window.render({t})")
                stage.screenshot(path=str(HERE / f"still_{t:g}.png"))
                print("wrote", HERE / f"still_{t:g}.png")
            browser.close()
            return

        t_end = page.evaluate("window.T_END")
        frames = int(t_end * args.fps)
        # Encode to a temp name and rename at the end: an MP4 isn't playable until ffmpeg has
        # finished writing it, so a half-rendered file must never sit at the final path.
        partial = OUT.with_suffix(".partial.mp4")
        ffmpeg = subprocess.Popen(
            [imageio_ffmpeg.get_ffmpeg_exe(), "-y", "-loglevel", "error",
             "-f", "image2pipe", "-framerate", str(args.fps), "-c:v", "mjpeg", "-i", "-",
             # JPEG frames are full-range; convert to standard TV-range yuv420p, or the file comes
             # out as yuvj420p, which Windows' player, QuickTime and some browsers won't play.
             "-vf", "scale=in_range=pc:out_range=tv,format=yuv420p",
             "-c:v", "libx264", "-preset", "slow", "-crf", "18", "-profile:v", "high",
             "-color_range", "tv", "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709",
             "-movflags", "+faststart", str(partial)],
            stdin=subprocess.PIPE,
        )
        for i in range(frames):
            page.evaluate(f"window.render({i / args.fps})")
            ffmpeg.stdin.write(stage.screenshot(type="jpeg", quality=92))
            if i % (args.fps * 10) == 0:
                print(f"frame {i}/{frames}", flush=True)
        ffmpeg.stdin.close()
        if ffmpeg.wait() != 0:
            raise SystemExit("ffmpeg failed; left " + str(partial))
        browser.close()
        partial.replace(OUT)
        print("wrote", OUT)


if __name__ == "__main__":
    main()
