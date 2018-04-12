FROM golang:1.9-alpine as builder
RUN apk add -U git
COPY ./cmd/cmd-server /go/src/github.com/gl-prototypes/cmd-localfs/cmd/cmd-server
WORKDIR /go/src/github.com/gl-prototypes/cmd-localfs/cmd/cmd-server
RUN set -ex \
    && go get . \
    && go install .


FROM alpine:3.6
RUN apk add -U sshfs
COPY --from=builder /go/bin/cmd-server .
COPY ./data/id_dev .
CMD ["/cmd-server"]
