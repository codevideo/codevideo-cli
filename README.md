# codevideo

The CLI tool for generating CodeVideos.

## Installation

```shell
git clone https://github.com/codevideo/codevideo
cd codevideo
cd puppeteer-runner
npm install
npx @puppeteer/browsers install chrome@latest
```

Make a note of what was installed in `puppeteer-runner/chrome` - you'll need to update the path in `puppeteer-runner/recordVideoV3.js` on line 73.

Then build the Go binary:

```shell
cd ../..
go build -o codevideo
```

Very importantly, create a `.env` file with your Elevenlabs API key:

```env
# Copy from .env.example and fill in your values
ELEVENLABS_API_KEY=your-elevenlabs-api-key
# ... other configuration options
```

You should be ready to start using the CodeVideo CLI!

If you don't have an Elevenlabs account - we're working on a solution with htgo-tts and other providers.

## Usage

With actions:

```shell
./codevideo -p "$(cat data/actions.json)"
```

Note: if you are using `zsh` and get the error `zsh: event not found: \`, try deactivating history expansion with `set +o histexpand` and try the command again.

If all works well, you should see the following final output:

```shell
Detected project type: Actions
/> CodeVideo generation in progress...
[==========================] 100% 
✅ CodeVideo successfully generated and saved to CodeVideo-2025-03-21-18-58-47.mp4
```

As an alternative, paste your actions, lesson, or course JSON into `data/actions.json`, `data/lesson.json`, or `data/course.json` respectively - all types are accepted.

With actions:

```shell
./codevideo -p "$(cat data/actions.json)"
```

With a lesson:

```shell
./codevideo -p "$(cat data/lesson.json)"
```

With a course:

```shell
./codevideo -p "$(cat data/course.json)"
```

## Complex CLI Example - Actions, With Given Output Path, and Open when Done

```shell
./codevideo -p "$(cat data/actions.json)" -o codevideo-intro.mp4 --open
```

## Video Configuration Options

You can specify the orientation and resolution of the video with the `-r` or `--resolution` and `-o` or `--orientation` flags, respectively. The default resolution is `1080p` and the default orientation is `landscape`.

## IDE Configuration Options

All React IDE props from the `CodeVideoIDE` can be passed in via the `-c` or `--config` to a config.json file. (See `data/config.json` for an example)

```shell
./codevideo -p "$(cat data/actions.json)" -c data/config.json
```

## Server usage:

Simply pass the `-m serve` parameter to the command to start the server:

```shell
./codevideo -m serve
```

To run in the background use `nohup` or similar:

```shell
nohup ./codevideo -m serve &
```

This will watch for manifest files in /tmp/v3/new and process them as they arrive. The server will output the video to the `output` folder.

## Docker 

Build the container

```shell
docker build -t codevideo .
```

Run in server mode (default)

```shell
docker run -p 8080:8080 -v $(pwd)/.env:/.env -v $(pwd)/output:/app/output codevideo
```

# Run with specific actions

```shell
docker run -v $(pwd)/.env:/.env -v $(pwd)/output:/app/output codevideo -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"}]"
```

## For Developers

You can update the Gatsby static site by replacing the `public` folder within `cli/staticserver`. We recommend you use the `example` site within the `example` folder of the [`@fullstackcraftllc/codevideo-ide-react`](https://github.com/codevideo/codevideo-ide-react) repository.

Everything in the `public` folder is treated as an embedded Go resource and served by the server.

## CodeVideo Studio

Build your actions JSON in the [CodeVideo Studio](https://studio.codevideo.io)!
