# codevideo-cli

The CLI tool for generating CodeVideos.

## Usage

1. Clone this repository:

```shell
git clone https://github.com/codevideo/codevideo-cli
```

2. Rename the `.env.example` file to `.env` and fill in the required fields:

```shell
cp .env.example .env
```

If you don't have an Elevenlabs account - we're working on a solution with htgo-tts and other providers.

3. Try a simple example video generation:

```shell
go run main.go -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"},{\"name\":\"author-speak-before\",\"value\":\"First, let's make a Python file.\"},{\"name\":\"file-explorer-create-file\",\"value\":\"main.py\"},{\"name\":\"file-explorer-open-file\",\"value\":\"main.py\"},{\"name\":\"author-speak-before\",\"value\":\"and let's print 'Hello world!' to the console.\"},{\"name\":\"editor-type\",\"value\":\"print('Hello, world!')\"}]"
```

Note: if you are using `zsh` and get the error `zsh: event not found: \`, try deactivating history expansion with `set +o histexpand` and try the command again.

If all works well, you should see the following final output:

```shell
Detected project type: Actions
/> CodeVideo generation in progress...
[==========================] 100% 
✅ CodeVideo successfully generated and saved to CodeVideo-2025-03-21-18-58-47.mp4
```

Alternatively build the prod version and run it:

```shell
go build -o codevideo
./codevideo -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"},{\"name\":\"author-speak-before\",\"value\":\"First, let's make a Python file.\"},{\"name\":\"file-explorer-create-file\",\"value\":\"main.py\"},{\"name\":\"file-explorer-open-file\",\"value\":\"main.py\"},{\"name\":\"author-speak-before\",\"value\":\"and let's print 'Hello world!' to the console.\"},{\"name\":\"editor-type\",\"value\":\"print('Hello, world!')\"}]"
```

As yet another alternative, paste your actions into `actions.json` and run the following command:

```shell
./codevideo -p "$(cat actions.json)"
```

## Server usage:

Simply pass the `-m serve` parameter to the command to start the server:

```shell
go build -o codevideo
./codevideo -m serve
```

To run in the background use `nohup` or similar:

```shell
nohup ./codevideo -m serve &
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

Update the Gatsby static site by replacing the public folder within `cli/staticserver`. Everything in the `public` folder is treated as an embedded go resource and served by the server.

## CodeVideo Studio

Build your actions JSON in the [CodeVideo Studio](https://studio.codevideo.io)!