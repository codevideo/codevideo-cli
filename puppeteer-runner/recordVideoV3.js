const { launch, getStream, wss } = require("puppeteer-stream");
const fs = require("fs");
const path = require("path");
const yargs = require('yargs/yargs');
const { hideBin } = require('yargs/helpers');

// define sleep helper function
const sleep = ms => new Promise(res => setTimeout(res, ms));

// Parse command line arguments
const argv = yargs(hideBin(process.argv))
  .option('uuid', {
    type: 'string',
    description: 'Path to the manifest file'
  })
  .option('os', {
    type: 'string',
    default: 'linux',
    description: 'Operating system (linux or mac)'
  })
  .option('resolution', {
    type: 'string',
    default: '1080p',
    description: 'Video resolution (1080p or 4K)'
  })
  .option('orientation', {
    type: 'string',
    default: 'landscape',
    description: 'Video orientation (landscape or portrait)'
  })
  .argv;

// parse uuid, resolution and orientation from command line arguments
const uuid = argv.uuid;
const os = argv.os;
const resolution = argv.resolution;
const orientation = argv.orientation;

// set width and height based on resolution and orientation
let width, height;
if (resolution === '4K') {
    width = orientation === 'landscape' ? 3840 : 2160;
    height = orientation === 'landscape' ? 2160 : 3840;
} else if (resolution === '1080p') {
    width = orientation === 'landscape' ? 1920 : 1080;
    height = orientation === 'landscape' ? 1080 : 1920;
}
// if no manifest is provided, exit
if (!uuid) {
    console.error("Please provide a manifest file path.");
    process.exit(1);
}

async function recordVideoV3() {
    console.log("Starting recording for UUID: ", uuid);

    const outputWebm = path.join(__dirname, `../../tmp/v3/video/${uuid}.webm`);
    const file = fs.createWriteStream(outputWebm);

    console.log("Launching browser with resolution:", width, "x", height, orientation, width === 3840 ? " (4K)" : " (1080p)");

    const browser = await launch({
        dumpio: true,
        // macOS:
        // executablePath: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
        // linux (docker):
        executablePath: os === "mac" ? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" : "/usr/bin/chromium-browser",
        headless: "new", // supports audio!
        // headless: false, // for debugging
        defaultViewport: { width, height },
        args: [
            `--window-size=${width},${height}`,
            '--start-fullscreen',
            `--ozone-override-screen-size=${width},${height}`, // for linux
            '--no-sandbox', // to run as root on docker
            '--autoplay-policy=no-user-gesture-required',
            // '--disable-web-security',
            // '--enable-logging=stderr',  // Enable detailed logging
            // '--v=1',                    // Increase verbosity level
        ],
    });

    const page = await browser.newPage();

    // Log domcontentloaded
    page.once('domcontentloaded', () => {
        console.log('DOM content loaded');
    });

    // Log load
    page.once('load', () => {
        console.log('Page fully loaded');
    });

    // Log all browser console messages.
    page.on('console', msg => {
        console.log('BROWSER LOG:', msg.text());
    });

    // Create a promise that resolves when the final progress update is received.
    let resolveFinalProgress;
    const finalProgressPromise = new Promise(resolve => {
        resolveFinalProgress = resolve;
    });

    // Expose __onActionProgress so that progress stats from the client are logged in Node.
    // When a final progress update is received, we resolve the promise.
    await page.exposeFunction('__onActionProgress', (progress) => {
        console.log("Progress update:", progress);
        // Check if this progress update indicates completion.
        if (progress.progress === "100.0" || progress.currentAction >= progress.totalActions) {
            resolveFinalProgress();
        }
    });
    console.log("added __onActionProgress");

    // Navigate to the puppeteer page.
    await page.setViewport({ width: 0, height: 0 });

    const url = `http://localhost:7001/v3?uuid=${uuid}`;
    console.log(`Navigating to ${url}`);
    await page.goto(url);
    console.log("Page navigated");

    // click body to trigger interaction
    await page.click("body");
    console.log("Clicked body");

    // Inject CSS to remove margins/padding so the video fills the viewport
    await page.addStyleTag({ content: `body { margin: 0; padding: 0; }` });
    console.log("Added style tag");

    const videoConstraints = {
        mandatory: {
            minWidth: width,
            minHeight: height,
            maxWidth: width,
            maxHeight: height,
        },
    };

    // Start the stream and pipe it to a file.
    const stream = await getStream(page, {
        audio: true,
        video: true,
        mimeType: "video/webm", // WebM is well-supported for high-quality web video
        audioBitsPerSecond: 384000, // 384 kbps for high-quality stereo audio
        videoBitsPerSecond: width === 3840 ?  80000000: 20000000, // 80 Mpbs for 4K; 20 Mbps (20,000 kbps) for high-quality 1080p video
        frameSize: 16, // approx 60 FPS
        videoConstraints
    });
    stream.pipe(file);
    console.log("Recording started");

    // Wait a moment before triggering start.
    await sleep(1000);

    // Send signal to react component to start recording
    console.log("Triggering recording start...");
    await page.evaluate(() => {
        window.__startRecording();
    })

    // Wait until the client sends a final progress update.
    await finalProgressPromise;

    // Wait a moment before stopping the recording.
    await sleep(1000);

    // Once complete, tear down the recording.
    console.log("Final progress received. Stopping recording...");
    await stream.destroy();
    file.close();
    console.log("Recording finished");

    await browser.close();
    (await wss).close();
}

recordVideoV3().catch(err => {
    console.error("Error during recording:", err);
});
