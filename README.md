# meet2twake

This script can be used as glue to put the transcripts of visio (meet) in Twake Drive.
Meet asks livekit to put the files in minIO. An event handler is declared in minIO to
sent a webhook to this script, which can then copy the files from minIO to Twake Drive.

## Configuration

The script can be configured via environment variables:

* `PORT`: the port on which the script listens for webhooks (8090 by default)
* `CLOUDERY_URL`: the URL of the cloudery (https://manager.cozycloud.cc/ by default)
* `CLOUDERY_TOKEN`: the TOKEN for making requests to the cloudery
