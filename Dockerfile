FROM golang:1.9-alpine as builder
RUN apk add -U git
COPY ./cmd/cmd-server /go/src/github.com/gl-prototypes/cmd-localfs/cmd/cmd-server
WORKDIR /go/src/github.com/gl-prototypes/cmd-localfs/cmd/cmd-server
RUN set -ex \
    && go get . \
    && go install .

FROM alpine:latest as sshfs
RUN apk add -U build-base \
	coreutils \
	fuse3-dev \
	git \
	glib-dev \
	meson \
	ninja \
	--repository http://dl-3.alpinelinux.org/alpine/edge/testing/
COPY sshfs /usr/src/sshfs
WORKDIR /usr/src/sshfs
RUN mkdir build \
	&& ( \
	cd build \
	&& meson .. \
	&& ninja \
	&& ninja install \
	)

FROM alpine:latest
RUN apk add -U fuse3 glib openssh-client \
	--repository http://dl-3.alpinelinux.org/alpine/edge/testing/
RUN ln -snf /usr/bin/fusermount3 /usr/bin/fusermount
COPY --from=builder /go/bin/cmd-server .
COPY --from=sshfs /usr/local/bin/sshfs /bin/sshfs
COPY --from=sshfs /usr/local/sbin/mount.sshfs /sbin/mount.sshfs
COPY --from=sshfs /usr/local/sbin/mount.fuse.sshfs /sbin/mount.fuse.sshfs
COPY ./data/id_dev .
CMD ["/cmd-server"]
