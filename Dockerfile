FROM golang:1.26-bookworm@sha256:e60d708a92ad26a6d61901334510d3debd23ddcba125663ecd6008d42e8ec669 AS builder

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

# Generate the third-party license notice from the build's own module cache,
# so it always matches the binary being shipped (no committed bundle to drift).
RUN go install github.com/google/go-licenses@v1.6.0 && \
    PATH="$PATH:$(go env GOPATH)/bin" ./scripts/gen-licenses.sh /THIRD_PARTY_LICENSES.txt

FROM node:24-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d

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
RUN cd /opt/technician/playwright && \
    npm ci && \
    npx playwright install --with-deps chromium && \
    chown -R technician:technician /opt/technician/playwright "$PLAYWRIGHT_BROWSERS_PATH"
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
