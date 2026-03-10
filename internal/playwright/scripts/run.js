#!/usr/bin/env node

/**
 * Technician Playwright Runner Harness
 *
 * Spawned by the Go orchestrator to run Playwright probe scripts.
 * Outputs structured JSON to stdout.
 *
 * Usage: node run.js '{"script": "/path/to/probe.js", "base_url": "...", "video": true}'
 */

const { chromium, devices } = require('playwright');
const path = require('path');
const fs = require('fs');
const { performance } = require('perf_hooks');

// Network throttling profiles (CDP emulateNetworkConditions)
const NETWORK_PROFILES = {
  '4g':      { offline: false, downloadThroughput: 4 * 1024 * 1024 / 8,  uploadThroughput: 3 * 1024 * 1024 / 8,  latency: 150 },
  '3g':      { offline: false, downloadThroughput: 1.5 * 1024 * 1024 / 8, uploadThroughput: 750 * 1024 / 8,       latency: 300 },
  'slow-3g': { offline: false, downloadThroughput: 500 * 1024 / 8,        uploadThroughput: 500 * 1024 / 8,       latency: 2000 },
};

async function main() {
  const configStr = process.argv[2];
  if (!configStr) {
    outputError('No config provided');
    process.exit(1);
  }

  let config;
  try {
    config = JSON.parse(configStr);
  } catch (e) {
    outputError(`Invalid config JSON: ${e.message}`);
    process.exit(1);
  }

  const startTime = performance.now();
  const logs = [];
  const log = (msg) => logs.push(msg);

  let browser;
  let context;
  let page;

  try {
    // Launch browser
    browser = await chromium.launch({
      headless: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox'],
    });

    // Create context with HAR recording
    const contextOpts = {
      recordHar: { path: '/tmp/technician-har.har', mode: 'full' },
    };

    // Apply device emulation (viewport, user agent, device scale factor)
    if (config.device && devices[config.device]) {
      Object.assign(contextOpts, devices[config.device]);
      log(`Device emulation: ${config.device}`);
    }

    if (config.video) {
      contextOpts.recordVideo = {
        dir: '/tmp/technician-videos/',
        size: { width: contextOpts.viewport?.width || 1280, height: contextOpts.viewport?.height || 720 },
      };
    }

    if (config.base_url) {
      contextOpts.baseURL = config.base_url;
    }

    // Handle authenticator (load storage state)
    if (config.authenticator) {
      const statePath = path.resolve(config.authenticator);
      if (fs.existsSync(statePath)) {
        contextOpts.storageState = statePath;
        log(`Loaded storage state from ${statePath}`);
      }
    }

    context = await browser.newContext(contextOpts);
    page = await context.newPage();

    // Apply network throttling via CDP
    if (config.network && NETWORK_PROFILES[config.network]) {
      const profile = NETWORK_PROFILES[config.network];
      const cdp = await context.newCDPSession(page);
      await cdp.send('Network.emulateNetworkConditions', profile);
      log(`Network throttling: ${config.network} (${profile.latency}ms RTT, ${Math.round(profile.downloadThroughput * 8 / 1024)}kbps down)`);
    }

    // Load and run the probe script
    const scriptPath = path.resolve(config.script);
    log(`Running script: ${scriptPath}`);

    const probeModule = require(scriptPath);
    const probeFn = probeModule.default || probeModule;

    if (typeof probeFn !== 'function') {
      throw new Error(`Probe script must export a function, got ${typeof probeFn}`);
    }

    const probeContext = {
      base_url: config.base_url || '',
      credentials: {},
      log,
    };

    await probeFn(page, probeContext);

    // Collect Web Vitals
    const vitals = await collectWebVitals(page);
    log('Collected Web Vitals');

    // Get video path
    let videoPath = '';
    if (config.video) {
      const video = page.video();
      if (video) {
        videoPath = await video.path();
      }
    }

    // Close context to finalize HAR
    await context.close();
    context = null;

    // Parse HAR
    let har = null;
    let resourceCount = 0;
    try {
      const harData = fs.readFileSync('/tmp/technician-har.har', 'utf8');
      const harJson = JSON.parse(harData);
      const entries = harJson.log.entries || [];
      resourceCount = entries.length;

      let totalTransfer = 0;
      const harEntries = entries.map((entry) => {
        const transferSize = entry.response.bodySize > 0
          ? entry.response.bodySize
          : entry.response.content.size || 0;
        totalTransfer += transferSize;

        return {
          url: entry.request.url,
          resource_type: classifyMime(entry.response.content.mimeType || ''),
          duration: entry.time,
          transfer_size: transferSize,
          response_size: entry.response.content.size || 0,
          status: entry.response.status,
        };
      });

      har = {
        entries: harEntries,
        total_transfer_bytes: totalTransfer,
      };
    } catch (e) {
      log(`HAR parsing failed: ${e.message}`);
    }

    const durationMs = performance.now() - startTime;

    output({
      success: true,
      duration_ms: durationMs,
      vitals,
      har,
      video_path: videoPath,
      resource_count: resourceCount,
      logs,
    });
  } catch (e) {
    const durationMs = performance.now() - startTime;
    output({
      success: false,
      duration_ms: durationMs,
      error: e.message,
      logs,
    });
  } finally {
    if (context) {
      await context.close().catch(() => {});
    }
    if (browser) {
      await browser.close().catch(() => {});
    }
  }
}

async function collectWebVitals(page) {
  const fallback = { ttfb: 0, fcp: 0, lcp: 0, cls: 0, inp: 0, dom_complete: 0 };
  try {
    // Register observers, then trigger visibility change to flush LCP/CLS
    const withLcpCls = await page.evaluate(async () => {
      const nav = performance.getEntriesByType('navigation')[0] || {};
      const paint = performance.getEntriesByType('paint');
      const fcpEntry = paint.find((p) => p.name === 'first-contentful-paint');
      const ttfb = nav.responseStart || 0;
      const fcp = fcpEntry ? fcpEntry.startTime : 0;
      const dom_complete = nav.domComplete || 0;

      try {
        const { onLCP, onCLS } = await import('https://unpkg.com/web-vitals@4?module');
        const result = { ttfb, fcp, dom_complete, lcp: 0, cls: 0 };

        const lcpDone = new Promise((resolve) => {
          onLCP((m) => { result.lcp = m.value; resolve(); }, { reportAllChanges: true });
        });
        const clsDone = new Promise((resolve) => {
          onCLS((m) => { result.cls = m.value; resolve(); }, { reportAllChanges: true });
        });

        // Force LCP/CLS to finalize by simulating a visibility change
        // (web-vitals reports LCP on visibilitychange or page hide)
        await new Promise((r) => setTimeout(r, 100));
        document.dispatchEvent(new Event('visibilitychange'));

        await Promise.race([
          Promise.all([lcpDone, clsDone]),
          new Promise((r) => setTimeout(r, 3000)),
        ]);
        return result;
      } catch {
        return { ttfb, fcp, dom_complete, lcp: 0, cls: 0 };
      }
    });

    // INP requires at least one interaction; trigger a click then read INP
    await page.click('body', { timeout: 2000 }).catch(() => {});
    await new Promise((r) => setTimeout(r, 300));

    const inp = await page.evaluate(async () => {
      try {
        const { onINP } = await import('https://unpkg.com/web-vitals@4?module');
        return new Promise((resolve) => {
          onINP((m) => resolve(m.value), { reportAllChanges: true });
          // Force INP to report via visibility change
          document.dispatchEvent(new Event('visibilitychange'));
          setTimeout(() => resolve(0), 1000);
        });
      } catch {
        return 0;
      }
    });

    return {
      ...withLcpCls,
      inp: typeof inp === 'number' ? inp : 0,
    };
  } catch {
    return fallback;
  }
}

function classifyMime(mime) {
  if (!mime) return 'other';
  if (mime.includes('html')) return 'document';
  if (mime.includes('javascript') || mime.includes('ecmascript')) return 'script';
  if (mime.includes('css')) return 'stylesheet';
  if (mime.includes('image')) return 'image';
  if (mime.includes('font') || mime.includes('woff') || mime.includes('ttf')) return 'font';
  if (mime.includes('json') || mime.includes('xml')) return 'xhr';
  return 'other';
}

function output(data) {
  process.stdout.write(JSON.stringify(data));
}

function outputError(msg) {
  output({ success: false, error: msg, duration_ms: 0 });
}

main().catch((e) => {
  outputError(`Unhandled error: ${e.message}`);
  process.exit(1);
});
