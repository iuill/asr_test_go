export type TranscriptEntry = { text: string; at: number };

export function formatEntry(entry: TranscriptEntry, timestamps: boolean): string {
  if (!timestamps) return entry.text;
  const at = new Date(entry.at);
  return `[${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}] ${entry.text}`;
}

export class TranscriptStore {
  private display = new Map<string, TranscriptEntry[]>();
  private session = new Map<string, TranscriptEntry[]>();

  beginRecording(): void { this.session.clear(); }
  clearDisplay(): void { this.display.clear(); }
  entries(id: string): TranscriptEntry[] { return this.display.get(id) || []; }
  sessionEntries(id: string): TranscriptEntry[] { return this.session.get(id) || []; }

  append(id: string, text: string, at = Date.now()): TranscriptEntry {
    const entry = { text, at };
    for (const store of [this.display, this.session]) {
      const entries = store.get(id) || [];
      entries.push(entry);
      store.set(id, entries);
    }
    return entry;
  }

  content(models: { id: string; name: string }[], timestamps: boolean, sessionOnly = false): string {
    const source = sessionOnly ? this.session : this.display;
    return models.map(model => {
      const entries = source.get(model.id) || [];
      return entries.length ? `${model.name}\n${entries.map(entry => formatEntry(entry, timestamps)).join('\n')}` : '';
    }).filter(Boolean).join('\n\n');
  }
}
