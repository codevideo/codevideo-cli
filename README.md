# codevideo-cli

The CLI tool for generating CodeVideos.

## Installation

### Download Pre-built Binaries

1. Go to the [Releases page](https://github.com/codevideo/codevideo-cli/releases) and download the appropriate binary for your operating system and architecture.

2. Make the binary executable (Linux/macOS only):

```shell
chmod +x codevideo-cli-*
```

3. Move the binary to a directory in your `PATH`:

```shell
mv codevideo-cli-* /usr/local/bin/codevideo
```
4. Verify the installation by running:

```shell
codevideo --version
```

### Build from Source

If you prefer to build from source or need the latest development version:

```shell
git clone https://github.com/codevideo/codevideo-cli
cd codevideo-cli
go build -o codevideo
```

## Configuration

Create a `.env` file in the same directory as your binary (or in your working directory):

```env
ELEVENLABS_API_KEY=your-elevenlabs-api-key
ELEVENLABS_VOICE_ID=your-elevenlabs-voice-id
```

The S3, Clerk, and Mailjet variables are only used in server mode and are not required for CLI usage.

The slack variable can be used to send notifications when a video is generated, but it is not required.

The application will automatically load this `.env` file when started.

## Usage

1. Create your `.env` file with the required fields (see Configuration section above).

If you don't have an Elevenlabs account - we're working on a solution with htgo-tts and other providers.

2. Try a simple example video generation:

```shell
./codevideo -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"},{\"name\":\"author-speak-before\",\"value\":\"First, let's make a Python file.\"},{\"name\":\"file-explorer-create-file\",\"value\":\"main.py\"},{\"name\":\"file-explorer-open-file\",\"value\":\"main.py\"},{\"name\":\"author-speak-before\",\"value\":\"and let's print 'Hello world!' to the console.\"},{\"name\":\"editor-type\",\"value\":\"print('Hello, world!')\"}]"
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
./codevideo -p "$(cat data/actions.json)" -o mysuperspecialvideo.mp4 --open
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

## Docker 

Build the container

```shell
docker build -t ./codevideo .
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

Everything in the `public` folder is treated as an embedded go resource and served by the server.

## CodeVideo Studio

Build your actions JSON in the [CodeVideo Studio](https://studio.codevideo.io)!
