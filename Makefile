
build:
	docker build -t cmd-localfs .

run-server:
	@docker run --rm -v /var:/mnt alpine sh -c "mkdir -p /mnt/sshfs"
	docker run -p 2222:2222 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/sshfs:/mnt/sshfs:shared \
		--privileged \
		cmd-localfs

client:
	go get ./cmd/cmd-client
	go build ./cmd/cmd-client
	./cmd-client localhost:2222
