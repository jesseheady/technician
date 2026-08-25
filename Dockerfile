FROM golang:1.27-bookworm@sha256:a9278c7936a41f7d33dae94784df33442e71fb4d6943a08100cf1898845c5bea AS builder

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
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /technician .

# Generate the third-party license notice from the build's own module cache,
# so it always matches the binary being shipped (no committed bundle to drift).
RUN go install github.com/google/go-licenses@v1.6.0 && \
    PATH="$PATH:$(go env GOPATH)/bin" ./scripts/gen-licenses.sh /THIRD_PARTY_LICENSES.txt

FROM node:24-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS runtime

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
COPY --from=builder /THIRD_PARTY_LICENSES.txt /usr/local/share/technician/THIRD_PARTY_LICENSES.txt

RUN mkdir -p /var/lib/technician /tmp/technician/artifacts /tmp/technician-videos && \
    chown -R technician:technician /var/lib/technician /tmp/technician /tmp/technician-videos

WORKDIR /
USER technician

EXPOSE 9590

ENTRYPOINT ["technician"]
CMD ["worker", "--config", "/etc/technician/technician.yml"]
