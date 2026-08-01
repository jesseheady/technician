FROM golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS builder

# Let the builder fetch the exact toolchain go.mod pins if it is newer than the
# base image, instead of hard-failing (golang images default to GOTOOLCHAIN=local).
# Downloads are verified against the Go checksum database; go.mod remains the pin.
# This decouples a go.mod `go` bump from the base-image digest bump so the two
# can land in separate PRs without breaking the image build.
ENV GOTOOLCHAIN=auto

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /technician .



FROM node:24-slim@sha256:235600a8101ab264e117b1768e925532262668dc9b581ef1dd7d96ced463b8e7

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    mtr-tiny \
    ca-certificates \
    libcap2-bin \
    wget \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --system --gid 1001 technician && \
    useradd --system --uid 1001 --gid technician --home-dir /home/technician --create-home --shell /usr/sbin/nologin technician

ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright-browsers

COPY internal/playwright/scripts/ /opt/technician/playwright/
# npm, npx, corepack, and yarn are build-time only: the runtime spawns `node`
# directly (internal/playwright/runner.go). They are removed in this same layer
# because npm's bundled dependency tree is ~143 of the image's node packages and
# is the source of nearly every fixable CVE the image scan reports — CVEs we
# cannot patch ourselves, since they ship inside the upstream node base image.
RUN cd /opt/technician/playwright && \
    npm ci && \
    npx playwright install --with-deps chromium && \
    chown -R technician:technician /opt/technician/playwright "$PLAYWRIGHT_BROWSERS_PATH" && \
    rm -rf /usr/local/lib/node_modules/npm \
           /usr/local/lib/node_modules/corepack \
           /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack \
           /opt/yarn-v* /usr/local/bin/yarn /usr/local/bin/yarnpkg \
           /root/.npm
ENV NODE_PATH=/opt/technician/playwright/node_modules

COPY --from=builder /technician /usr/local/bin/technician
RUN setcap cap_net_raw+ep /usr/local/bin/technician

# Project license (MIT) and third-party notices for the statically linked Go
# dependencies, generated in the builder stage. Playwright's own notices ship
# under /opt/technician/playwright/node_modules.
COPY LICENSE /usr/local/share/technician/
COPY THIRD_PARTY_LICENSES-linux.txt /usr/local/share/technician/THIRD_PARTY_LICENSES-linux.txt

RUN mkdir -p /var/lib/technician /tmp/technician/artifacts /tmp/technician-videos && \
    chown -R technician:technician /var/lib/technician /tmp/technician /tmp/technician-videos

WORKDIR /
USER technician

EXPOSE 9590

ENTRYPOINT ["technician"]
CMD ["worker", "--config", "/etc/technician/technician.yml"]
