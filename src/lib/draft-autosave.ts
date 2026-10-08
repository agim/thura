import type { DraftInput, MailItem } from '@lidza/client'

// One writer per editor. A response for an older snapshot cannot mark newer
// text saved; flush drains all edits before the caller may queue a send.
export class DraftAutosave {
  private revision = 0
  private savedRevision = 0
  private timer?: ReturnType<typeof setTimeout>
  private running?: Promise<MailItem>
  private input: DraftInput
  private result: MailItem

  constructor(item: MailItem, private persist: (input: DraftInput) => Promise<MailItem>, private report: (state: string, error: string) => void) {
    this.input = { to: item.toAddress, cc: item.cc, bcc: item.bcc, subject: item.subject, text: item.textBody, threadId: item.threadId }
    this.result = item
  }

  get dirty() { return this.revision !== this.savedRevision }

  update(input: DraftInput) {
    this.input = { ...input }
    this.revision++
    this.report('Unsaved changes', '')
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { void this.flush().catch(() => {}) }, 750)
  }

  flush(): Promise<MailItem> {
    clearTimeout(this.timer)
    if (this.running) return this.running
    this.running = this.drain().finally(() => { this.running = undefined })
    return this.running
  }

  private async drain() {
    try {
      while (this.dirty) {
        const revision = this.revision
        const input = { ...this.input }
        this.report('Saving draft…', '')
        this.result = await this.persist(input)
        this.savedRevision = revision
      }
      this.report('Draft saved', '')
      return this.result
    } catch (error) {
      this.report('Draft not saved', error instanceof Error ? error.message : String(error))
      throw error
    }
  }
}
