# meet2twake

This script can be used as glue to put the transcripts and recordings of visio
(meet) in Twake Drive.

![diagram](docs/diagram.svg)

## Configuration

The script can be configured via environment variables:

* `LOG_LEVEL`: `debug`, `info` (default), `warn`, or `error`
* `PORT`: the port on which the script listens for webhooks (8090 by default)
* `CLOUDERY_URL`: the URL of the cloudery (`https://manager.cozycloud.cc/` by default)
* `CLOUDERY_TOKEN`: the TOKEN for making requests to the cloudery
* `MINIO_URL`: the URL of minIO API (like `https://minio.example.com`)
* `MINIO_USER`: the username for minIO (like `minio`)
* `MINIO_PASSWORD`: the password for minIO
* `MINIO_INSECURE`: `true` to skip the TLS certificate check
* `POSTGRES_URL`: the URL of the postgresql database (like `postgres://username:password@localhost:5432/database_name`)

For the Postgresql connection, it's also possible to use env variables listed on
https://www.postgresql.org/docs/current/libpq-envars.html
