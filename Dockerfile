cat Dockerfile 
FROM golang:1.25 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server .

FROM scratch
WORKDIR /
COPY --from=builder /server /server
EXPOSE 8090
ENTRYPOINT ["/server"]
