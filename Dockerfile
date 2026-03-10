FROM golang:1.25-bookworm AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /technician .

FROM node:22-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    mtr-tiny \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY internal/playwright/scripts/ /opt/technician/playwright/
RUN cd /opt/technician/playwright && npm init -y && npm install playwright && npx playwright install --with-deps chromium
ENV NODE_PATH=/opt/technician/playwright/node_modules

COPY --from=builder /technician /usr/local/bin/technician

WORKDIR /
RUN mkdir -p /tmp/technician/artifacts /tmp/technician-videos

EXPOSE 9394

ENTRYPOINT ["technician"]
CMD ["worker", "--config", "/etc/technician/technician.yml"]
