/** A stand-in EventSource that connects nowhere; a test sends it events as a run's stream would. */
export class FakeEventSource extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static opened: FakeEventSource[] = [];

  readonly url: string;
  readonly withCredentials: boolean;
  readyState = FakeEventSource.OPEN;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string | URL, init?: EventSourceInit) {
    super();
    this.url = String(url);
    this.withCredentials = init?.withCredentials ?? false;
    FakeEventSource.opened.push(this);
  }

  /** The stream opened last, which a page just started following. */
  static last(): FakeEventSource {
    const source = FakeEventSource.opened.at(-1);
    if (!source) throw new Error("nothing opened an event stream");
    return source;
  }

  /** Sends one event as the server would, by its type, with its row as the data. */
  send(type: string, data: unknown): void {
    if (this.readyState === FakeEventSource.CLOSED) return;
    this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data) }));
  }

  close(): void {
    this.readyState = FakeEventSource.CLOSED;
  }
}
