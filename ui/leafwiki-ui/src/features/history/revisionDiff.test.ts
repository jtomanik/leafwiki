import { describe, expect, it } from 'vitest'
import { buildLineDiff } from './revisionDiff'

describe('buildLineDiff', () => {
  it('reports exact line additions and removals for normal comparisons', () => {
    const diff = buildLineDiff('alpha\nbeta\ngamma', 'alpha\nbravo\ngamma')

    expect(diff.limited).toBe(false)
    expect(diff.summary).toEqual({ addedLines: 1, removedLines: 1 })
    expect(diff.lines.map((line) => [line.kind, line.value])).toEqual([
      ['context', 'alpha'],
      ['removed', 'beta'],
      ['added', 'bravo'],
      ['context', 'gamma'],
    ])
  })

  it('does not build a quadratic matrix for oversized comparisons', () => {
    const base = Array.from({ length: 4 }, (_, index) => `old ${index}`).join(
      '\n',
    )
    const target = Array.from({ length: 4 }, (_, index) => `new ${index}`).join(
      '\n',
    )

    const diff = buildLineDiff(base, target, { maxMatrixCells: 9 })

    expect(diff.limited).toBe(true)
    expect(diff.lines).toEqual([])
    expect(diff.summary).toEqual({ addedLines: 4, removedLines: 4 })
    expect(diff.limitReason).toContain('too large')
  })
})
