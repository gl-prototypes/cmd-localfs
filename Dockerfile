FROM golang:1.9-alpine as builder
RUN apk add -U git
COPY ./cmd/ssh-server /go/src/github.com/gl-prototypes/localfs-docker-ssh/cmd/ssh-server
WORKDIR /go/src/github.com/gl-prototypes/localfs-docker-ssh/cmd/ssh-server
RUN set -ex \
    && go get . \
    && go install .


FROM alpine:3.6
RUN apk add -U sshfs
COPY --from=builder /go/bin/ssh-server .
COPY ./data/id_dev .
CMD ["/ssh-server"]
