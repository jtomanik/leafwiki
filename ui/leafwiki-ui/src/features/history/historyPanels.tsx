import {
  type RevisionComparison,
  type RevisionSnapshot,
} from '@/lib/api/revisions'
import { workspaceAssetPath } from '@/lib/workspaceAssets'
import { useCallback } from 'react'
import MarkdownPreview from '../preview/MarkdownPreview'
import type { LineDiff } from './revisionDiff'

function SummaryStat({
  label,
  value,
  emphasized = false,
  tone = 'default',
}: {
  label: string
  value: string
  emphasized?: boolean
  tone?: 'default' | 'added' | 'removed'
}) {
  return (
    <div
      className={`page-history__summary-stat page-history__summary-stat--${tone} ${
        emphasized ? 'page-history__summary-stat--emphasized' : ''
      }`.trim()}
    >
      <div className="page-history__summary-stat-value">{value}</div>
      <div className="page-history__summary-stat-label">{label}</div>
    </div>
  )
}

function DiffView({
  comparison,
  diff,
}: {
  comparison: RevisionComparison
  diff: LineDiff
}) {
  if (!comparison.contentChanged) {
    return (
      <div className="page-history__empty-message">
        No text difference between this revision and the active version.
      </div>
    )
  }

  if (diff.limited) {
    return (
      <div className="page-history__empty-message">
        {diff.limitReason ??
          'This text difference is too large to render in the browser.'}
      </div>
    )
  }

  return (
    <div className="page-history__diff">
      {diff.lines.map((line, index) => (
        <div
          key={`${line.kind}-${line.oldLineNumber}-${line.newLineNumber}-${index}`}
          className={`page-history__diff-line page-history__diff-line--${line.kind}`}
        >
          <span className="page-history__diff-gutter">
            {line.oldLineNumber ?? ''}
          </span>
          <span className="page-history__diff-gutter">
            {line.newLineNumber ?? ''}
          </span>
          <span className="page-history__diff-marker">
            {line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '}
          </span>
          <code className="page-history__diff-content">
            {line.value || ' '}
          </code>
        </div>
      ))}
    </div>
  )
}

export function ChangesPanel({
  comparison,
  diff,
}: {
  comparison: RevisionComparison
  diff: LineDiff
}) {
  return (
    <div className="page-history__detail-stack">
      <section className="page-history__summary">
        <div className="page-history__section-heading">Change Summary</div>
        <div className="page-history__summary-grid">
          <SummaryStat
            label="Lines added since"
            value={String(diff.summary.addedLines)}
            emphasized={diff.summary.addedLines > 0}
            tone="added"
          />
          <SummaryStat
            label="Lines removed since"
            value={String(diff.summary.removedLines)}
            emphasized={diff.summary.removedLines > 0}
            tone="removed"
          />
        </div>
      </section>

      <section className="page-history__section">
        <div className="page-history__section-heading">
          Diff{' '}
          <span className="page-history__section-heading-note">
            compared to the active version
          </span>
        </div>
        <DiffView comparison={comparison} diff={diff} />
      </section>
    </div>
  )
}

export function PreviewPanel({
  snapshot,
  workspaceId,
}: {
  snapshot: RevisionSnapshot
  workspaceId: string
}) {
  const resolveAssetUrl = useCallback(
    (src: string) => {
      return workspaceAssetPath(src, workspaceId)
    },
    [workspaceId],
  )

  return (
    <div className="page-history__preview-panel custom-scrollbar">
      <div className="page-history__preview-body">
        <MarkdownPreview
          content={snapshot.content}
          path={snapshot.revision.path}
          pageKind={snapshot.revision.kind === 'section' ? 'section' : 'page'}
          workspaceId={workspaceId}
          resolveAssetUrl={resolveAssetUrl}
          enableHeadlineLinks={false}
        />
      </div>
    </div>
  )
}

export function RawTextPanel({ snapshot }: { snapshot: RevisionSnapshot }) {
  return (
    <div className="page-history__detail-stack">
      <section className="page-history__section">
        <div className="page-history__section-heading">Raw Text</div>
        <div className="custom-scrollbar markdown-code-block page-history__raw-text-block">
          <pre className="custom-scrollbar page-history__snapshot-content">
            <code>{snapshot.content || '(empty)'}</code>
          </pre>
        </div>
      </section>
    </div>
  )
}
