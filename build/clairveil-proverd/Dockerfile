FROM golang:1.25.10-alpine AS build
ARG TARGETARCH=amd64

RUN apk add --no-cache git build-base linux-headers

WORKDIR /workspace
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /out/clairveil-proverd ./cmd/clairveil-proverd

FROM alpine:3.20 AS runtime

RUN adduser -D -g '' proveruser
RUN apk add --no-cache ca-certificates wget

COPY --from=build /out/clairveil-proverd /usr/bin/clairveil-proverd

USER proveruser
WORKDIR /home/proveruser

EXPOSE 8080

ENTRYPOINT ["/usr/bin/clairveil-proverd"]
CMD ["-listen", "0.0.0.0:8080"]
