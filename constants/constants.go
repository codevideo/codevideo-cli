package constants

import "time"

const (
	NEW_FOLDER                   = "../tmp/v3/new"
	ERROR_FOLDER                 = "../tmp/v3/error"
	SUCCESS_FOLDER               = "../tmp/v3/success"
	VIDEO_FOLDER                 = "../tmp/v3/video"
	NODE_SCRIPT_NAME             = "puppeteer-runner/recordVideoV3.js"
	TOKEN_DECREMENT_AMOUNT       = 10
	MAX_CONCURRENT_JOBS          = 2 // because we only have an 8 GB server which serves both staging and prod requests, keep at 2 each
	DEFAULT_MANIFEST_SERVER_PORT = 7000
	DEFAULT_GATSBY_PORT          = 7001
	DEFAULT_SERVER_TIMEOUT       = time.Second * 5
)
