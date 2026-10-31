export interface CheckItemUI {
  id: string;
  program: string;
  args: string[];
  argsError?: string;
  definitionPathsText?: string;
  dir: string;
  timeoutSeconds: number;
}

export function parseArgsString(str: string): { args: string[]; error?: string } {
  if (!str) return { args: [] };
  const trimmed = str.trim();
  if (!trimmed) return { args: [] };

  const args: string[] = [];
  let current = '';
  let inDouble = false;
  let inSingle = false;
  let escaped = false;
  let hasToken = false;

  for (let i = 0; i < str.length; i++) {
    const ch = str[i];

    if (escaped) {
      current += ch;
      escaped = false;
      hasToken = true;
      continue;
    }

    if (ch === '\\' && !inSingle) {
      escaped = true;
      hasToken = true;
      continue;
    }

    if (inDouble) {
      if (ch === '"') {
        inDouble = false;
      } else {
        current += ch;
      }
      hasToken = true;
      continue;
    }

    if (inSingle) {
      if (ch === "'") {
        inSingle = false;
      } else {
        current += ch;
      }
      hasToken = true;
      continue;
    }

    if (ch === '"') {
      inDouble = true;
      hasToken = true;
      continue;
    }

    if (ch === "'") {
      inSingle = true;
      hasToken = true;
      continue;
    }

    if (/\s/.test(ch)) {
      if (hasToken) {
        args.push(current);
        current = '';
        hasToken = false;
      }
      continue;
    }

    current += ch;
    hasToken = true;
  }

  if (escaped) {
    return {
      args: [],
      error: '参数存在未完成的反斜杠转义',
    };
  }

  if (inDouble || inSingle) {
    return {
      args: [],
      error: inDouble ? '参数存在未闭合的双引号' : '参数存在未闭合的单引号',
    };
  }

  if (hasToken) {
    args.push(current);
  }

  return { args };
}

export function formatArgsString(args?: string[]): string {
  if (!args || !args.length) return '';
  return args
    .map((arg) => {
      if (arg === '') {
        return '""';
      }
      const needsQuotes = /[\s\t\n"'\\]/.test(arg);
      if (!needsQuotes) {
        return arg;
      }
      const escaped = arg
        .replace(/\\/g, '\\\\')
        .replace(/"/g, '\\"');
      return `"${escaped}"`;
    })
    .join(' ');
}

export function checksToUI(checks?: any[]): CheckItemUI[] {
  if (!Array.isArray(checks) || !checks.length) return [];
  return checks.map((c, idx) => {
    const id = (c && typeof c.id === 'string' && c.id.trim()) ? c.id.trim() : `check-${idx + 1}`;
    const argv = Array.isArray(c?.argv) ? c.argv : [];
    const program = argv[0] || '';
    const rawArgs = argv.slice(1).map((a: any) => String(a ?? ''));
    return {
      id,
      program,
      args: rawArgs,
      definitionPathsText: Array.isArray(c?.definitionPaths) ? c.definitionPaths.join('\n') : '',
      dir: c?.dir || '.',
      timeoutSeconds: Number(c?.timeoutSeconds) > 0 ? Number(c.timeoutSeconds) : 300,
    };
  });
}

export function generateNextCheckId(existingItems: { id: string }[]): string {
  let maxNum = 0;
  for (const item of existingItems) {
    const m = /^check-(\d+)$/.exec(item.id.trim());
    if (m) {
      const n = parseInt(m[1], 10);
      if (n > maxNum) maxNum = n;
    }
  }
  let candidate = `check-${maxNum + 1}`;
  const existingSet = new Set(existingItems.map((i) => i.id.trim()));
  let offset = 1;
  while (existingSet.has(candidate)) {
    candidate = `check-${maxNum + 1 + offset}`;
    offset++;
  }
  return candidate;
}

export function uiToChecks(items: CheckItemUI[]): any[] {
  return items
    .filter((item) => item.program.trim() !== '')
    .map((item) => {
      const argv = [item.program.trim(), ...item.args];
      const definitionPaths = (item.definitionPathsText || '').split(/\r?\n/).map(p => p.trim()).filter(Boolean);
      return {
        id: item.id.trim() || 'check',
        ...(definitionPaths.length ? { definitionPaths } : {}),
        argv,
        dir: item.dir.trim() || '.',
        timeoutSeconds: Number(item.timeoutSeconds) > 0 ? Number(item.timeoutSeconds) : 300,
      };
    });
}

export function parseGitHubRepo(url?: string): { owner: string; name: string } {
  if (!url) return { owner: '', name: '' };
  const raw = url.trim();
  try {
    const clean = raw
      .replace(/^https?:\/\/github\.com\//, '')
      .replace(/^git@github\.com:/, '')
      .replace(/^ssh:\/\/git@github\.com\//, '')
      .replace(/\.git$/, '');
    const parts = clean.split('/');
    if (parts.length >= 2) {
      return {
        owner: parts[0] || '',
        name: parts[1] || '',
      };
    }
  } catch {}
  return { owner: '', name: '' };
}

export function isEquivalentRepo(url1?: string, url2?: string): boolean {
  if (!url1 || !url2) return false;
  const p1 = parseGitHubRepo(url1);
  const p2 = parseGitHubRepo(url2);
  if (!p1.owner || !p1.name || !p2.owner || !p2.name) return false;
  return p1.owner.toLowerCase() === p2.owner.toLowerCase() && p1.name.toLowerCase() === p2.name.toLowerCase();
}
