FROM golang:1.25 AS builder
WORKDIR /app
COPY . .
RUN go build -o /server .

FROM distroless:latest
WORKDIR /
COPY --from=builder /server /server
EXPOSE 8090
ENTRYPOINT ["/server"]
