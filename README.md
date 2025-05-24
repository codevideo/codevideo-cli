# codevideo-cli

The CLI tool for generating CodeVideos.

## Installation

### Download Pre-built Binaries

1. Go to the [Releases page](https://github.com/codevideo/codevideo-cli/releases)
2. Download the appropriate binary for your system:
   - **Windows (64-bit Intel/AMD)**: `codevideo-cli-windows-amd64.exe`
   - **Windows (ARM64)**: `codevideo-cli-windows-arm64.exe`
   - **Linux (64-bit Intel/AMD)**: `codevideo-cli-linux-amd64`
   - **Linux (ARM64)**: `codevideo-cli-linux-arm64`
   - **macOS (Intel)**: `codevideo-cli-darwin-amd64`
   - **macOS (Apple Silicon)**: `codevideo-cli-darwin-arm64`

3. Make the binary executable (Linux/macOS only):
   ```bash
   chmod +x codevideo-cli-*
   ```

4. Optionally, rename and move to your PATH:
   ```bash
   # Linux/macOS - rename and move to system bin
   sudo mv codevideo-cli-* /usr/local/bin/codevideo
   
   # Or add to your user bin
   mkdir -p ~/bin
   mv codevideo-cli-* ~/bin/codevideo
   ```

### Quick Install Script

**Linux/macOS:**
```bash
curl -s https://api.github.com/repos/codevideo/codevideo-cli/releases/latest \
| grep "browser_download_url.*$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)" \
| cut -d '"' -f 4 \
| xargs curl -L -o codevideo && chmod +x codevideo
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
# Copy from .env.example and fill in your values
ELEVENLABS_API_KEY=your-elevenlabs-api-key
# ... other configuration options
```

The application will automatically load this `.env` file when started.

## Usage

1. Create your `.env` file with the required fields (see Configuration section above).

If you don't have an Elevenlabs account - we're working on a solution with htgo-tts and other providers.

2. Try a simple example video generation:

```shell
codevideo -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"},{\"name\":\"author-speak-before\",\"value\":\"First, let's make a Python file.\"},{\"name\":\"file-explorer-create-file\",\"value\":\"main.py\"},{\"name\":\"file-explorer-open-file\",\"value\":\"main.py\"},{\"name\":\"author-speak-before\",\"value\":\"and let's print 'Hello world!' to the console.\"},{\"name\":\"editor-type\",\"value\":\"print('Hello, world!')\"}]"
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
codevideo -p "$(cat data/actions.json)"
```

With a lesson:

```shell
codevideo -p "$(cat data/lesson.json)"
```

With a course:

```shell
codevideo -p "$(cat data/course.json)"
```

## Complex CLI Example - Actions, With Given Output Path, and Open when Done

```shell
codevideo -p "$(cat data/actions.json)" -o mysuperspecialvideo.mp4 --open
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
