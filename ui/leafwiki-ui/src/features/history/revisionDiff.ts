export type DiffLine = {
  kind: 'context' | 'added' | 'removed'
  value: string
  oldLineNumber: number | null
  newLineNumber: number | null
}

export type DiffSummary = {
  addedLines: number
  removedLines: number
}

export type LineDiff = {
  lines: DiffLine[]
  summary: DiffSummary
  limited: boolean
  limitReason?: string
}

export type LineDiffOptions = {
  maxMatrixCells?: number
  maxRenderedLines?: number
}

const DEFAULT_MAX_MATRIX_CELLS = 250_000
const DEFAULT_MAX_RENDERED_LINES = 2_000

function splitContentLines(content: string): string[] {
  return content.split('\n')
}

function limitedDiff(
  baseLines: string[],
  targetLines: string[],
  reason: string,
): LineDiff {
  return {
    lines: [],
    summary: {
      addedLines: targetLines.length,
      removedLines: baseLines.length,
    },
    limited: true,
    limitReason: reason,
  }
}

export function buildLineDiff(
  baseContent: string,
  targetContent: string,
  options: LineDiffOptions = {},
): LineDiff {
  const baseLines = splitContentLines(baseContent)
  const targetLines = splitContentLines(targetContent)
  const rows = baseLines.length
  const cols = targetLines.length
  const matrixCells = (rows + 1) * (cols + 1)
  const maxMatrixCells = options.maxMatrixCells ?? DEFAULT_MAX_MATRIX_CELLS

  if (matrixCells > maxMatrixCells) {
    return limitedDiff(
      baseLines,
      targetLines,
      `Diff is too large to render safely (${matrixCells.toLocaleString()} cells).`,
    )
  }

  const matrix = new Uint32Array(matrixCells)

  const index = (row: number, col: number) => row * (cols + 1) + col

  for (let row = rows - 1; row >= 0; row -= 1) {
    for (let col = cols - 1; col >= 0; col -= 1) {
      if (baseLines[row] === targetLines[col]) {
        matrix[index(row, col)] = matrix[index(row + 1, col + 1)] + 1
      } else {
        matrix[index(row, col)] = Math.max(
          matrix[index(row + 1, col)],
          matrix[index(row, col + 1)],
        )
      }
    }
  }

  const lines: DiffLine[] = []
  let row = 0
  let col = 0
  let oldLineNumber = 1
  let newLineNumber = 1
  let addedLines = 0
  let removedLines = 0

  while (row < rows && col < cols) {
    if (baseLines[row] === targetLines[col]) {
      lines.push({
        kind: 'context',
        value: baseLines[row],
        oldLineNumber,
        newLineNumber,
      })
      row += 1
      col += 1
      oldLineNumber += 1
      newLineNumber += 1
      continue
    }

    if (matrix[index(row + 1, col)] >= matrix[index(row, col + 1)]) {
      lines.push({
        kind: 'removed',
        value: baseLines[row],
        oldLineNumber,
        newLineNumber: null,
      })
      row += 1
      oldLineNumber += 1
      removedLines += 1
      continue
    }

    lines.push({
      kind: 'added',
      value: targetLines[col],
      oldLineNumber: null,
      newLineNumber,
    })
    col += 1
    newLineNumber += 1
    addedLines += 1
  }

  while (row < rows) {
    lines.push({
      kind: 'removed',
      value: baseLines[row],
      oldLineNumber,
      newLineNumber: null,
    })
    row += 1
    oldLineNumber += 1
    removedLines += 1
  }

  while (col < cols) {
    lines.push({
      kind: 'added',
      value: targetLines[col],
      oldLineNumber: null,
      newLineNumber,
    })
    col += 1
    newLineNumber += 1
    addedLines += 1
  }

  const maxRenderedLines =
    options.maxRenderedLines ?? DEFAULT_MAX_RENDERED_LINES

  if (lines.length > maxRenderedLines) {
    return {
      lines: [],
      summary: { addedLines, removedLines },
      limited: true,
      limitReason: `Diff has too many lines to render safely (${lines.length.toLocaleString()} lines).`,
    }
  }

  return {
    lines,
    summary: { addedLines, removedLines },
    limited: false,
  }
}
