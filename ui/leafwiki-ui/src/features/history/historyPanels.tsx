import { Button } from '@/components/ui/button'
import {
  buildRevisionAssetUrl,
  type RevisionAssetChange,
  type RevisionComparison,
  type RevisionSnapshot,
} from '@/lib/api/revisions'
import { withBasePath } from '@/lib/routePath'
import { workspaceAssetPath } from '@/lib/workspaceAssets'
import { Download, ExternalLink, FileText } from 'lucide-react'
import { useCallback } from 'react'
import { AssetPreviewTooltip } from '../assets/AssetPreviewTooltip'
import MarkdownPreview from '../preview/MarkdownPreview'
import type { LineDiff } from './revisionDiff'

function assetChangeLabel(status: RevisionAssetChange['status']) {
  switch (status) {
    case 'added':
      return 'Added'
    case 'removed':
      return 'Removed'
    case 'modified':
      return 'Replaced'
    default:
      return status
  }
}

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
  gitBackedWorkspace,
}: {
  comparison: RevisionComparison
  diff: LineDiff
  gitBackedWorkspace: boolean
}) {
  const assetSummary = {
    added: 0,
    modified: 0,
    removed: 0,
  }
  comparison.assetChanges.forEach((change) => {
    assetSummary[change.status] += 1
  })

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
          {!gitBackedWorkspace ? (
            <SummaryStat
              label="Assets changed"
              value={String(comparison.assetChanges.length)}
              emphasized={comparison.assetChanges.length > 0}
            />
          ) : null}
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

      {!gitBackedWorkspace && comparison.assetChanges.length > 0 ? (
        <details className="page-history__asset-details">
          <summary className="page-history__asset-summary">
            Assets ({comparison.assetChanges.length})
          </summary>
          <div className="page-history__asset-list">
            {comparison.assetChanges.map((change) => (
              <div
                key={`${change.name}-${change.status}`}
                className="page-history__asset-change"
              >
                <span className="page-history__asset-name">{change.name}</span>
                <span className="page-history__asset-meta">
                  {assetChangeLabel(change.status)}
                </span>
              </div>
            ))}
          </div>
          <div className="page-history__asset-summary-row">
            {assetSummary.added > 0 ? (
              <span>{assetSummary.added} added</span>
            ) : null}
            {assetSummary.modified > 0 ? (
              <span>{assetSummary.modified} replaced</span>
            ) : null}
            {assetSummary.removed > 0 ? (
              <span>{assetSummary.removed} removed</span>
            ) : null}
          </div>
        </details>
      ) : null}
    </div>
  )
}

export function PreviewPanel({
  snapshot,
  gitBackedWorkspace,
  workspaceId,
}: {
  snapshot: RevisionSnapshot
  gitBackedWorkspace: boolean
  workspaceId: string
}) {
  const pageId = snapshot.revision.pageId
  const revisionId = snapshot.revision.id

  const resolveAssetUrl = useCallback(
    (src: string) => {
      if (gitBackedWorkspace) return workspaceAssetPath(src, workspaceId)

      const normalizedSrc = src.startsWith('assets/') ? `/${src}` : src
      const assetPrefix = `/assets/${pageId}/`

      if (!normalizedSrc.startsWith(assetPrefix)) {
        return src
      }

      return buildRevisionAssetUrl(
        pageId,
        revisionId,
        normalizedSrc.slice(assetPrefix.length),
        workspaceId,
      )
    },
    [gitBackedWorkspace, pageId, revisionId, workspaceId],
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

function HistoryAssetItem({
  asset,
  pageId,
  revisionId,
  workspaceId,
}: {
  asset: RevisionSnapshot['assets'][number]
  pageId: string
  revisionId: string
  workspaceId: string
}) {
  const assetUrl = withBasePath(
    buildRevisionAssetUrl(pageId, revisionId, asset.name, workspaceId),
  )
  const baseName = asset.name.split('/').pop() ?? asset.name

  return (
    <li className="group asset-item page-history__asset-item">
      <div className="flex min-w-0 flex-1 items-center gap-1">
        <AssetPreviewTooltip url={assetUrl} name={baseName}>
          {asset.mimeType?.startsWith('image/') ? (
            <img
              src={assetUrl}
              alt={baseName}
              className="asset-item__preview-image"
            />
          ) : (
            <div className="asset-item__preview-file">
              <FileText size={18} />
            </div>
          )}
        </AssetPreviewTooltip>

        <div className="page-history__asset-copy">
          <span className="asset-item__filename">{baseName}</span>
          <span className="page-history__asset-copy-meta">
            {asset.mimeType || 'application/octet-stream'} ·{' '}
            {Intl.NumberFormat().format(asset.sizeBytes)} bytes
          </span>
        </div>
      </div>

      <Button
        asChild
        variant="outline"
        size="icon"
        className="asset-item__action-button"
      >
        <a
          href={assetUrl}
          target="_blank"
          rel="noreferrer"
          title="Open asset"
          data-testid={`history-asset-open-${baseName}`}
        >
          <ExternalLink size={16} />
        </a>
      </Button>
      <Button
        asChild
        variant="outline"
        size="icon"
        className="asset-item__action-button"
      >
        <a
          href={assetUrl}
          download={baseName}
          title="Download asset"
          data-testid={`history-asset-download-${baseName}`}
        >
          <Download size={16} />
        </a>
      </Button>
    </li>
  )
}

export function AssetsPanel({
  snapshot,
  workspaceId,
}: {
  snapshot: RevisionSnapshot
  workspaceId: string
}) {
  return (
    <div className="page-history__detail-stack">
      <section className="page-history__section">
        <div className="page-history__section-heading">Assets</div>
        {snapshot.assets.length === 0 ? (
          <div className="page-history__empty-message">
            No assets were stored with this revision.
          </div>
        ) : (
          <ul className="page-history__asset-list">
            {snapshot.assets.map((asset) => (
              <HistoryAssetItem
                key={`${asset.name}-${asset.sha256}`}
                asset={asset}
                pageId={snapshot.revision.pageId}
                revisionId={snapshot.revision.id}
                workspaceId={workspaceId}
              />
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
