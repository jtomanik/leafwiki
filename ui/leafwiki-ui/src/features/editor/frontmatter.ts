import {
  asApiErrorCode,
  asMessageID,
  type ApiErrorCode,
  type MessageID,
} from '@/lib/semanticTypes'

const INTERNAL_FIELD_PREFIX = 'leafwiki_'

export type EditorFrontmatterFieldType = 'text' | 'number' | 'boolean' | 'list'

export type EditorFrontmatterField = {
  key: string
  value: string
  type: EditorFrontmatterFieldType
  internal?: boolean
}

export type EditorFrontmatterValidationError = {
  code: ApiErrorCode
  messageId: MessageID
  message: string
}

export type EditorFrontmatterValidationErrors = Record<
  string,
  EditorFrontmatterValidationError
>

function validationError(
  code: string,
  messageId: string,
  message: string,
): EditorFrontmatterValidationError {
  return {
    code: asApiErrorCode(code),
    messageId: asMessageID(messageId),
    message,
  }
}

function normalizeTag(tag: string) {
  return tag.trim().toLocaleLowerCase()
}

function normalizeFieldKey(key: string) {
  const trimmed = key.trim()
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"')) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  ) {
    return trimmed.slice(1, -1).trim()
  }
  return trimmed
}

function normalizeListValue(value: string) {
  return value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
    .join('\n')
}

export function normalizeTags(tags: string[]) {
  const seen = new Set<string>()
  const result: string[] = []

  for (const tag of tags.map(normalizeTag).filter(Boolean)) {
    const key = tag.toLocaleLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    result.push(tag)
  }

  return result
}

export function normalizeEditorFrontmatterFields(
  fields: EditorFrontmatterField[],
) {
  const seen = new Set<string>()
  const result: EditorFrontmatterField[] = []

  for (const field of fields) {
    const key = normalizeFieldKey(field.key)
    if (!key) continue

    const dedupeKey = key.toLocaleLowerCase()
    if (seen.has(dedupeKey)) continue
    seen.add(dedupeKey)

    const normalizedValue =
      field.type === 'list'
        ? normalizeListValue(field.value)
        : field.value.trim()

    result.push({
      key,
      type: field.type,
      internal: field.internal,
      value:
        field.type === 'boolean'
          ? normalizedValue === 'false'
            ? 'false'
            : 'true'
          : normalizedValue,
    })
  }

  return result
}

export function validateEditorFrontmatterMetadata(
  tags: string[],
  fields: EditorFrontmatterField[],
): EditorFrontmatterValidationErrors {
  const errors: EditorFrontmatterValidationErrors = {}
  const seenTags = new Set<string>()
  const seenKeys = new Map<string, number>()

  for (const tag of tags) {
    if (tag.trim() !== tag) {
      errors.tags = validationError(
        'frontmatter_tags_whitespace',
        'validation.frontmatter.tags.whitespace',
        'Tags must not contain leading or trailing whitespace.',
      )
      break
    }

    if (tag.trim() === '') {
      errors.tags = validationError(
        'frontmatter_tags_empty',
        'validation.frontmatter.tags.empty',
        'Tags must not be empty.',
      )
      break
    }

    const key = tag.toLocaleLowerCase()
    if (seenTags.has(key)) {
      errors.tags = validationError(
        'frontmatter_tags_duplicate',
        'validation.frontmatter.tags.duplicate',
        'Tags must be unique.',
      )
      break
    }
    seenTags.add(key)
  }

  fields.forEach((field, index) => {
    if (field.internal) return

    const keyField = `properties.${index}.key`
    const valueField = `properties.${index}.value`
    const trimmedKey = field.key.trim()

    if (trimmedKey === '') {
      errors[keyField] = validationError(
        'frontmatter_property_key_empty',
        'validation.frontmatter.property_key.empty',
        'Property key must not be empty.',
      )
      return
    }

    if (trimmedKey !== field.key) {
      errors[keyField] = validationError(
        'frontmatter_property_key_whitespace',
        'validation.frontmatter.property_key.whitespace',
        'Property key must not contain leading or trailing whitespace.',
      )
      return
    }

    if (trimmedKey.toLocaleLowerCase().startsWith(INTERNAL_FIELD_PREFIX)) {
      errors[keyField] = validationError(
        'frontmatter_property_key_reserved_prefix',
        'validation.frontmatter.property_key.reserved_prefix',
        'Property key uses a reserved prefix.',
      )
      return
    }

    const lowerKey = trimmedKey.toLocaleLowerCase()
    if (lowerKey === 'tags' || lowerKey === 'title') {
      errors[keyField] = validationError(
        'frontmatter_property_key_reserved',
        'validation.frontmatter.property_key.reserved',
        'Property key is reserved.',
      )
      return
    }

    const dedupeKey = trimmedKey.toLocaleLowerCase()
    const existingIndex = seenKeys.get(dedupeKey)
    if (existingIndex !== undefined) {
      const duplicateError = validationError(
        'frontmatter_property_key_duplicate',
        'validation.frontmatter.property_key.duplicate',
        'Property key must be unique.',
      )
      errors[keyField] = duplicateError
      if (!errors[`properties.${existingIndex}.key`]) {
        errors[`properties.${existingIndex}.key`] = duplicateError
      }
      return
    }
    seenKeys.set(dedupeKey, index)

    if (field.type === 'list') {
      return
    }

    if (typeof field.value !== 'string') {
      errors[valueField] = validationError(
        'frontmatter_property_value_type',
        'validation.frontmatter.property_value.type',
        'Property value must be a string, number, boolean, or flat list.',
      )
    }
  })

  return errors
}
