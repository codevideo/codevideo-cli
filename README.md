# codevideo

The CLI tool for generating CodeVideos.

## Installation

### Option 1: Homebrew (Recommended for macOS)

```bash
brew tap codevideo/codevideo
brew install codevideo
```

After installation, use the tool as `codevideo`:

```bash
codevideo --help
```

### Option 2: Download Pre-built Binaries

1. Go to the [Releases page](https://github.com/codevideo/codevideo/releases) and download the appropriate binary for your OS:

2. Make the binary executable (Linux/macOS only):

```bash
chmod +x codevideo-*
```

3. Optionally, rename and move to your PATH:

```bash
# Linux/macOS - rename and move to system bin
sudo mv codevideo-* /usr/local/bin/codevideo

# Or add to your user bin
mkdir -p ~/bin
mv codevideo-* ~/bin/codevideo
```

### Option 3: Build from Source

If you prefer to build from source or need the latest development version:

```shell
git clone https://github.com/codevideo/codevideo
cd codevideo
go build -o codevideo
```

## Configuration

Create a `.env` file in your **working directory** (the directory where you plan to run codevideo):

```env
# Copy from .env.example and fill in your values
ELEVENLABS_API_KEY=your-elevenlabs-api-key
# ... other configuration options
```

**Important:** The `.env` file is loaded from your current working directory when you run the command, not from where the binary is installed.

### Configuration Options:

1. **Project-specific .env file** (recommended):
   ```bash
   cd /path/to/your/project
   echo "ELEVENLABS_API_KEY=your-key" > .env
   codevideo -p "your-actions"
   ```

2. **Global environment variables:**
   ```bash
   export ELEVENLABS_API_KEY=your-key
   codevideo -p "your-actions"
   ```

3. **Home directory .env file:**
   ```bash
   echo "ELEVENLABS_API_KEY=your-key" > ~/.env
   cd ~ && codevideo -p "your-actions"
   ```

## Usage

1. Create your `.env` file with the required fields (see Configuration section above).

If you don't have an Elevenlabs account - we're working on a solution with htgo-tts and other providers.

2. Try a simple example video generation:

```shell
codevideo -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"},{\"name\":\"author-speak-before\",\"value\":\"First, let's make a Python file.\"},{\"name\":\"file-explorer-create-file\",\"value\":\"main.py\"},{\"name\":\"file-explorer-open-file\",\"value\":\"main.py\"},{\"name\":\"author-speak-before\",\"value\":\"and let's print 'Hello world!' to the console.\"},{\"name\":\"editor-type\",\"value\":\"print('Hello, world!')\"}]"
```

Note: if you are using `zsh` and get the error `zsh: event not found: \`, try deactivate history expansion with `set +o histexpand` and try the command again.

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
codevideo -m serve
```

To run in the background use `nohup` or similar:

```shell
nohup codevideo -m serve &
```

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

Everything in the `public` folder is treated as an embedded go resource and served by the server.

## CodeVideo Studio

Build your actions JSON in the [CodeVideo Studio](https://studio.codevideo.io)!
