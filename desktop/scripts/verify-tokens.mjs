import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const SRC_DIR = path.resolve(__dirname, '../src');
const CSS_PATH = path.resolve(SRC_DIR, 'styles/anthropic.css');

const BANNED_HEX_RE = /#(?:000000|ffffff|30d158|ff3b30)\b/gi;
const BANNED_MASTER_RE = /\bMasterTrader\b|master_trader|master_position/g;
const BANNED_ORDER_CTA_RE =
  /(?:\b(?:place[_\s-]?order|submit[_\s-]?order|auto[_\s-]?(?:order|trade)|execute[_\s-]?(?:order|trade)|market[_\s-]?(?:buy|sell)|buy[_\s-]?now|sell[_\s-]?now|one[_\s-]?click[_\s-]?(?:buy|sell|order))\b|自动下单|一键下单|市价买入|市价卖出|立即买入|立即卖出)/gi;
const BANNED_BUTTON_BUY_SELL_RE =
  /<(?:button|a)\b[^>]*>\s*(?:buy|sell|place\s+order|submit\s+order|下单|买入|卖出)\s*<\/(?:button|a)>/gi;

const REQUIRED_TOKENS = [
  '--bg-canvas',
  '--bg-surface',
  '--bg-elevated',
  '--border-hairline',
  '--text-primary',
  '--text-secondary',
  '--text-muted',
  '--accent-brand',
  '--candle-bull',
  '--candle-bear',
  '--accent-info',
  '--state-warn',
  '[data-theme="light"]',
  '.tabular-nums',
];

function walkFiles(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walkFiles(full));
    } else if (/\.(ts|tsx|css)$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

function collectMatches(filePath, content, re) {
  const hits = [];
  const lines = content.split('\n');
  for (let idx = 0; idx < lines.length; idx += 1) {
    const line = lines[idx];
    re.lastIndex = 0;
    const match = re.exec(line);
    if (match) {
      hits.push(`${path.relative(SRC_DIR, filePath)}:${idx + 1} -> ${match[0]}`);
    }
  }
  return hits;
}

function main() {
  if (!fs.existsSync(SRC_DIR)) {
    console.error(`verify-tokens: missing source directory ${SRC_DIR}`);
    process.exit(1);
  }
  if (!fs.existsSync(CSS_PATH)) {
    console.error(`verify-tokens: missing theme stylesheet ${CSS_PATH}`);
    process.exit(1);
  }

  const cssText = fs.readFileSync(CSS_PATH, 'utf8');
  const missingTokens = REQUIRED_TOKENS.filter((token) => !cssText.includes(token));
  if (missingTokens.length > 0) {
    console.error('verify-tokens: anthropic.css missing required tokens:', missingTokens);
    process.exit(1);
  }

  const files = walkFiles(SRC_DIR);
  const violations = [];
  let tabularUsageCount = 0;

  for (const file of files) {
    const content = fs.readFileSync(file, 'utf8');
    violations.push(...collectMatches(file, content, BANNED_HEX_RE));
    violations.push(...collectMatches(file, content, BANNED_MASTER_RE));
    violations.push(...collectMatches(file, content, BANNED_ORDER_CTA_RE));
    violations.push(...collectMatches(file, content, BANNED_BUTTON_BUY_SELL_RE));

    if (!file.endsWith('anthropic.css') && /\btabular-nums\b/.test(content)) {
      tabularUsageCount += 1;
    }
  }

  if (tabularUsageCount === 0) {
    violations.push('No TypeScript/TSX file references .tabular-nums for numeric alignment');
  }

  if (violations.length > 0) {
    console.error('verify-tokens FAILED:');
    for (const v of violations) {
      console.error(`  - ${v}`);
    }
    process.exit(1);
  }

  console.log(
    `verify-tokens OK: scanned ${files.length} files (${tabularUsageCount} files using tabular-nums), zero policy/token violations.`,
  );
}

main();
