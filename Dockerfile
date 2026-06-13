# Consumed by GoReleaser: it copies the already cross-compiled binary out of the
# build context rather than compiling, so the image build is fast and ships the
# same static binary every other artifact does.
FROM alpine:3.21

ARG TARGETPLATFORM

# git and go let the scaffolder initialise a repo and tidy modules from inside
# the image; ca-certificates for HTTPS submodule fetches.
RUN apk add --no-cache ca-certificates git \
 && adduser -D -H -u 10001 any

COPY $TARGETPLATFORM/any /usr/bin/any

USER any
WORKDIR /work

ENTRYPOINT ["/usr/bin/any"]
