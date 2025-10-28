# meet2twake

This script can be used as glue to put the transcripts and recordings of visio
(meet) in Twake Drive.

![diagram](docs/diagram.svg)

## Configuration

The script can be configured via environment variables:

* `LOG_LEVEL`: `debug`, `info` (default), `warn`, or `error`
* `PORT`: the port on which the script listens for webhooks (8090 by default)
* `TOKEN`: a token to protect the /minio and /transcript endpoints (must be sent as `Authorization: Bearer $TOKEN`)
* `CLOUDERY_URL`: the URL of the cloudery (`https://manager.cozycloud.cc/` by default)
* `CLOUDERY_TOKEN`: the TOKEN for making requests to the cloudery
* `MINIO_URL`: the URL of minIO API (like `https://minio.example.com`)
* `MINIO_USER`: the username for minIO (like `minio`)
* `MINIO_PASSWORD`: the password for minIO
* `MINIO_USE_SSL`: `true` to use HTTPS, `false` to use HTTP (default: `true`)
* `MINIO_INSECURE`: `true` to skip the TLS certificate check
* `MAIL_FROM`: the email from which the mail will be sent (`noreply@linagora.com` by default)
* `MAIL_SMTP`: the SMTP host to send the mail (`smtp.linagora.com` by default)
* `MAIL_PORT`: the port of the SMTP server (25 by default)
* `MAIL_DISABLE_TLS`; `true` to disable TLS for the SMTP server
* `AI_BASE_URL`: the OpenAI compatible endpoint base URL (`https://chat.lucie.ovh.linagora.com/` by default)
* `AI_API_KEY`: the API key to use for AI
* `AI_MODEL`: the model to use for generating the summary (`gpt-oss-120b` by default)

### PostgreSQL Configuration

You can configure PostgreSQL connection in two ways:

**Option 1: Using POSTGRES_URL (connection string)**
* `POSTGRES_URL`: the URL of the postgresql database (like `postgres://username:password@localhost:5432/database_name`)
  - Note: Special characters in username/password must be URL-encoded

**Option 2: Using separate variables (recommended)**
* `POSTGRES_HOST`: the PostgreSQL host (like `localhost` or `postgres.namespace.svc.cluster.local`)
* `POSTGRES_PORT`: the PostgreSQL port (default: `5432`)
* `POSTGRES_USER`: the PostgreSQL username
* `POSTGRES_PASSWORD`: the PostgreSQL password (no encoding needed)
* `POSTGRES_DB`: the database name
* `POSTGRES_SSLMODE`: SSL mode (`disable`, `require`, `verify-ca`, `verify-full`; default: `require`)

The separate variables approach is recommended as it handles special characters in passwords automatically.
