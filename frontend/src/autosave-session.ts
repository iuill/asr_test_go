type Backend = {
  begin: () => Promise<string>;
  write: (content: string) => Promise<string>;
  end: (content: string) => Promise<string>;
};

export type SaveEvent =
  | { type: 'started' | 'saved'; path: string }
  | { type: 'error'; error: unknown };

// Begin, writes, and end share one queue, including while begin is still pending.
export class AutoSaveSession {
  private queue: Promise<void> = Promise.resolve();
  private accepting = false;
  private currentPath = '';
  private backend: Backend;
  private notify: (event: SaveEvent) => void;

  constructor(backend: Backend, notify: (event: SaveEvent) => void) {
    this.backend = backend;
    this.notify = notify;
  }

  get path(): string { return this.currentPath; }

  begin(): Promise<void> {
    if (this.accepting) return this.queue;
    this.accepting = true;
    return this.enqueue(async () => {
      this.currentPath = await this.backend.begin();
      this.notify({ type: 'started', path: this.currentPath });
    });
  }

  write(content: () => string): Promise<void> {
    if (!this.accepting) return this.queue;
    return this.enqueue(async () => {
      if (!this.currentPath) return;
      const path = await this.backend.write(content());
      this.notify({ type: 'saved', path });
    });
  }

  end(content: () => string): Promise<void> {
    this.accepting = false;
    return this.enqueue(async () => {
      if (!this.currentPath) return;
      try {
        const path = await this.backend.end(content());
        this.notify({ type: 'saved', path });
      } finally {
        this.currentPath = '';
      }
    });
  }

  private enqueue(operation: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(operation).catch(error => this.notify({ type: 'error', error }));
    return this.queue;
  }
}
