const SEMANTIC_CAST_HELPERS = new Set([
  'asApiErrorCode',
  'asCommitHash',
  'asMCPAPIKeyID',
  'asMarkdownPath',
  'asMessageID',
  'asPageID',
  'asPageVersion',
  'asRevisionID',
  'asRoutePath',
  'asSessionID',
  'asSlug',
  'asUserID',
  'asWorkspaceID',
])

const OPERATIONAL_PROSE_PATTERNS = [
  /workspace synced, but some markdown files could not be loaded/i,
  /route path conflict/i,
  /import plan cleared/i,
  /start a new import to clear this result/i,
  /creating keys is unavailable for http remote-user sign-in/i,
]

const TOAST_METHODS = new Set(['error', 'message', 'success', 'warning'])
const STATUS_METADATA_KEYS = new Set([
  'data-error-code',
  'data-import-status',
  'data-l10n-id',
  'data-validation-code',
  'errorCode',
  'importStatus',
  'l10nId',
  'messageId',
  'validationCode',
])

function getFilename(context) {
  return (context.filename || context.getFilename?.() || '').replaceAll('\\', '/')
}

function isFrontendBoundaryFile(filename) {
  return (
    filename.includes('/src/lib/api/') ||
    filename.startsWith('src/lib/api/') ||
    filename.includes('/src/stores/') ||
    filename.startsWith('src/stores/') ||
    filename.endsWith('src/lib/usePresenceHeartbeat.ts') ||
    filename === 'src/lib/usePresenceHeartbeat.ts'
  )
}

function isSemanticTypesFile(filename) {
  return filename.endsWith('src/lib/semanticTypes.ts') || filename === 'src/lib/semanticTypes.ts'
}

function isE2EFile(filename) {
  return (
    filename.includes('/e2e/tests/') ||
    filename.includes('/e2e/pages/') ||
    filename.startsWith('tests/') ||
    filename.startsWith('pages/')
  )
}

function isApiOrRouteNormalizationFile(filename) {
  return (
    filename.includes('/src/lib/api/') ||
    filename.startsWith('src/lib/api/') ||
    filename.includes('/src/lib/route') ||
    filename.startsWith('src/lib/route') ||
    filename.includes('/src/lib/workspaceRoute') ||
    filename.startsWith('src/lib/workspaceRoute') ||
    filename.endsWith('src/lib/semanticTypes.ts') ||
    filename === 'src/lib/semanticTypes.ts'
  )
}

function getPropertyName(key) {
  if (!key) return null
  if (key.type === 'Identifier') return key.name
  if (key.type === 'Literal') return String(key.value)
  return null
}

function getNodeName(node) {
  if (!node) return null
  if (node.type === 'Identifier') return node.name
  if (node.type === 'RestElement') return getNodeName(node.argument)
  if (node.type === 'AssignmentPattern') return getNodeName(node.left)
  if (node.type === 'TSPropertySignature') return getPropertyName(node.key)
  if (node.type === 'PropertyDefinition') return getPropertyName(node.key)
  return null
}

function getEnclosingName(node, type) {
  let current = node.parent
  while (current) {
    if (current.type === type) return getNodeName(current.id) || null
    current = current.parent
  }
  return null
}

function getTypeSemanticContextName(node) {
  const names = []
  let current = node.parent

  while (current) {
    if (current.type === 'TSPropertySignature') {
      const name = getPropertyName(current.key)
      if (name) names.push(name)
    } else if (
      current.type === 'TSTypeAliasDeclaration' ||
      current.type === 'TSInterfaceDeclaration'
    ) {
      const name = getNodeName(current.id)
      if (name) names.push(name)
      break
    }
    current = current.parent
  }

  return names.join(' ')
}

function typeContainsRawString(typeNode) {
  if (!typeNode) return false
  switch (typeNode.type) {
    case 'TSStringKeyword':
      return true
    case 'TSArrayType':
      return typeContainsRawString(typeNode.elementType)
    case 'TSUnionType':
      return typeNode.types.some(typeContainsRawString)
    case 'TSIntersectionType':
      return typeNode.types.some(typeContainsRawString)
    case 'TSTypeReference':
      return (
        typeNode.typeParameters?.params?.some(typeContainsRawString) ||
        typeNode.typeArguments?.params?.some(typeContainsRawString) ||
        false
      )
    case 'TSTypeLiteral':
      return typeNode.members?.some((member) =>
        typeContainsRawString(member.typeAnnotation?.typeAnnotation),
      )
    case 'TSParenthesizedType':
      return typeContainsRawString(typeNode.typeAnnotation)
    default:
      return false
  }
}

function semanticTypeForName(name, contextName = '') {
  if (!name) return null
  const normalized = name.replaceAll('_', '').toLowerCase()
  const context = contextName.toLowerCase()

  if (normalized.includes('importplanid')) return 'ImportPlanID'
  if (normalized === 'id' && /(author|user)/.test(context)) return 'UserID'
  if (normalized.includes('workspaceid') || normalized === 'workspacetrees') {
    return 'WorkspaceID'
  }
  if (normalized.includes('sessionid')) return 'SessionID'
  if (normalized.includes('userid')) return 'UserID'
  if (normalized.includes('commithash') || normalized.includes('commitid')) {
    return 'CommitHash'
  }
  if (normalized.includes('slug') || normalized.includes('desiredslug')) return 'Slug'
  if (normalized.includes('routepath')) return 'RoutePath'
  if (normalized.includes('markdownpath')) return 'MarkdownPath'
  if (
    normalized.includes('pageid') ||
    normalized.includes('nodeid') ||
    normalized.includes('parentid') ||
    normalized === 'byid' ||
    normalized === 'id' && /(page|tree|node)/.test(context) ||
    normalized === 'existingid'
  ) {
    return 'PageID'
  }
  if (normalized === 'id' && context.includes('workspace')) return 'WorkspaceID'
  if (normalized === 'id' && context.includes('user')) return 'UserID'
  if (normalized === 'id' && context.includes('importplan')) return 'ImportPlanID'
  if (normalized === 'path' && context.includes('workspaceapipath')) return 'RoutePath'
  if (normalized.includes('treehash')) return 'CommitHash'

  return null
}

function hasBrand(program, brandName) {
  return program.body.some(
    (statement) =>
      statement.type === 'ExportNamedDeclaration' &&
      statement.declaration?.type === 'TSTypeAliasDeclaration' &&
      statement.declaration.id.name === brandName,
  )
}

function isSemanticCall(node) {
  if (node.type !== 'CallExpression') return false
  if (node.callee.type === 'Identifier') return SEMANTIC_CAST_HELPERS.has(node.callee.name)
  return false
}

function literalText(node) {
  if (!node) return null
  if (node.type === 'Literal' && typeof node.value === 'string') return node.value
  if (node.type === 'TemplateLiteral' && node.expressions.length === 0) {
    return node.quasis.map((quasi) => quasi.value.cooked).join('')
  }
  return null
}

function staticTextExpression(node) {
  const direct = literalText(node)
  if (direct !== null) return direct

  if (node?.type === 'ConditionalExpression') {
    const consequent = staticTextExpression(node.consequent)
    const alternate = staticTextExpression(node.alternate)
    if (consequent !== null && alternate !== null) return `${consequent}\n${alternate}`
  }

  return null
}

function isOperationalProse(text) {
  return OPERATIONAL_PROSE_PATTERNS.some((pattern) => pattern.test(text))
}

function memberPropertyName(node) {
  if (!node || node.type !== 'MemberExpression') return null
  if (!node.computed && node.property.type === 'Identifier') return node.property.name
  if (node.computed && node.property.type === 'Literal') return String(node.property.value)
  return null
}

function calleePropertyName(node) {
  if (!node || node.type !== 'CallExpression') return null
  return memberPropertyName(node.callee)
}

function objectHasStatusMetadata(node) {
  if (!node || node.type !== 'ObjectExpression') return false

  return node.properties.some((property) => {
    if (property.type !== 'Property') return false
    return STATUS_METADATA_KEYS.has(getPropertyName(property.key) || '')
  })
}

function isStoreOrStoreHelperFile(filename) {
  const basename = filename.split('/').pop()?.toLowerCase() || ''
  return (
    filename.includes('/src/stores/') ||
    filename.startsWith('src/stores/') ||
    basename.includes('store')
  )
}

function expectSubjectName(node) {
  if (node.type !== 'CallExpression') return null
  if (calleePropertyName(node) !== 'toContainText') return null
  const receiver = node.callee.object
  if (receiver?.type !== 'CallExpression') return null
  if (receiver.callee.type !== 'Identifier' || receiver.callee.name !== 'expect') {
    return null
  }
  const subject = receiver.arguments[0]
  return subject?.type === 'Identifier' ? subject.name : null
}

function isWorkspaceSyncStatusLocatorCall(node) {
  return (
    node?.type === 'CallExpression' &&
    calleePropertyName(node) === 'getByTestId' &&
    literalText(node.arguments[0]) === 'workspace-sync-status'
  )
}

function isWorkspaceSyncStatusTextAssertion(node, sourceCode, aliases) {
  if (node.type !== 'CallExpression') return false
  if (calleePropertyName(node) !== 'toContainText') return false
  const receiverText = sourceCode.getText(node.callee.object)
  const subjectName = expectSubjectName(node)
  if (
    !receiverText.includes('workspace-sync-status') &&
    (!subjectName || !aliases.has(subjectName))
  ) {
    return false
  }

  const asserted = node.arguments[0]
  if (literalText(asserted)) return true
  return (
    asserted?.type === 'Identifier' &&
    ['conflictDir', 'missingSlug'].includes(asserted.name)
  )
}

const noRawSemanticIdentifiers = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Require branded semantic types for frontend boundary identifiers.',
    },
    schema: [],
    messages: {
      rawSemanticIdentifier:
        'Use a branded semantic type for "{{name}}" instead of raw string at this frontend boundary.',
      missingSessionID:
        'semanticTypes.ts must define the {{brandName}} brand before planned semantic identifiers are accepted.',
    },
  },
  create(context) {
    const filename = getFilename(context)
    if (!isFrontendBoundaryFile(filename) && !isSemanticTypesFile(filename)) {
      return {}
    }

    function reportIfRaw(node, name, contextName, typeNode) {
      if (!typeContainsRawString(typeNode)) return
      const semanticType = semanticTypeForName(name, contextName)
      if (!semanticType) return
      context.report({
        node,
        messageId: 'rawSemanticIdentifier',
        data: { name: `${name} (${semanticType})` },
      })
    }

    return {
      Program(node) {
        if (!isSemanticTypesFile(filename)) return
        for (const brandName of ['SessionID', 'ImportPlanID']) {
          if (!hasBrand(node, brandName)) {
            context.report({
              node,
              loc: { line: 1, column: 0 },
              messageId: 'missingSessionID',
              data: { brandName },
            })
          }
        }
      },
      TSPropertySignature(node) {
        if (isSemanticTypesFile(filename)) return
        const name = getPropertyName(node.key)
        const contextName = getTypeSemanticContextName(node)
        reportIfRaw(node.key, name, contextName, node.typeAnnotation?.typeAnnotation)
      },
      Identifier(node) {
        if (isSemanticTypesFile(filename)) return
        if (!node.typeAnnotation) return

        const functionContext =
          getEnclosingName(node, 'FunctionDeclaration') ||
          getEnclosingName(node, 'TSMethodSignature') ||
          ''
        reportIfRaw(
          node,
          node.name,
          functionContext,
          node.typeAnnotation.typeAnnotation,
        )
      },
      FunctionDeclaration(node) {
        if (isSemanticTypesFile(filename)) return
        const name = node.id?.name
        reportIfRaw(node.id || node, name, name, node.returnType?.typeAnnotation)
      },
      CallExpression(node) {
        if (isSemanticTypesFile(filename)) return
        if (node.callee.type !== 'Identifier') return
        if (!['useRef', 'useState'].includes(node.callee.name)) return
        const variableName =
          node.parent?.type === 'VariableDeclarator'
            ? getNodeName(node.parent.id)
            : null
        const typeArguments =
          node.typeParameters?.params || node.typeArguments?.params || []
        for (const typeArgument of typeArguments) {
          reportIfRaw(node, variableName, '', typeArgument)
        }
      },
    }
  },
}

const noUnsafeSemanticCast = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Keep semantic cast helpers in API, route, or wire-normalization code.',
    },
    schema: [],
    messages: {
      unsafeCast:
        '{{name}} is a semantic normalization cast; move it to route/API/wire normalization instead of store or helper code.',
    },
  },
  create(context) {
    const filename = getFilename(context)
    if (isApiOrRouteNormalizationFile(filename)) return {}
    if (!isStoreOrStoreHelperFile(filename)) return {}

    return {
      CallExpression(node) {
        if (!isSemanticCall(node)) return
        context.report({
          node: node.callee,
          messageId: 'unsafeCast',
          data: { name: node.callee.name },
        })
      },
    }
  },
}

const noLocalizedProseAssertions = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Require E2E tests to assert semantic status metadata instead of operational prose.',
    },
    schema: [],
    messages: {
      proseAssertion:
        'Assert semantic status metadata instead of localized operational prose.',
      validationMessageShape:
        'E2E validation helpers should assert semantic validation codes, not message prose fields.',
    },
  },
  create(context) {
    const filename = getFilename(context)
    if (!isE2EFile(filename)) return {}
    const sourceCode = context.sourceCode || context.getSourceCode()
    const workspaceSyncStatusAliases = new Set()

    return {
      VariableDeclarator(node) {
        if (node.id.type !== 'Identifier') return
        if (isWorkspaceSyncStatusLocatorCall(node.init)) {
          workspaceSyncStatusAliases.add(node.id.name)
        }
      },
      CallExpression(node) {
        const propertyName = calleePropertyName(node)
        if (!propertyName) return

        if (propertyName === 'getByText') {
          const text = literalText(node.arguments[0])
          if (text && isOperationalProse(text)) {
            context.report({ node, messageId: 'proseAssertion' })
          }
          return
        }

        if (['toContainText', 'toHaveText'].includes(propertyName)) {
          const text = literalText(node.arguments[0])
          if (text && isOperationalProse(text)) {
            context.report({ node: node.arguments[0], messageId: 'proseAssertion' })
            return
          }
          if (
            isWorkspaceSyncStatusTextAssertion(
              node,
              sourceCode,
              workspaceSyncStatusAliases,
            )
          ) {
            context.report({ node: node.arguments[0], messageId: 'proseAssertion' })
          }
        }
      },
      Property(node) {
        if (getPropertyName(node.key) !== 'message') return
        if (
          node.value.type === 'CallExpression' &&
          calleePropertyName(node.value) === 'stringContaining'
        ) {
          context.report({ node: node.key, messageId: 'validationMessageShape' })
        }
      },
      TSPropertySignature(node) {
        if (getPropertyName(node.key) !== 'message') return
        const enclosingType = getEnclosingName(node, 'TSTypeAliasDeclaration') || ''
        if (
          filename.includes('importer.spec.ts') ||
          filename.includes('workspace-sync.spec.ts') ||
          enclosingType.toLowerCase().includes('validation')
        ) {
          context.report({ node: node.key, messageId: 'validationMessageShape' })
        }
      },
    }
  },
}

const requireSemanticStatusMetadata = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Require semantic metadata for static frontend operational toast copy.',
    },
    schema: [],
    messages: {
      missingMetadata:
        'Static operational toast copy must carry semantic status metadata such as messageId, errorCode, validationCode, or importStatus.',
    },
  },
  create(context) {
    const filename = getFilename(context)
    if (!filename.includes('/src/') && !filename.startsWith('src/')) return {}

    return {
      CallExpression(node) {
        if (node.callee.type !== 'MemberExpression') return
        if (node.callee.object.type !== 'Identifier' || node.callee.object.name !== 'toast') {
          return
        }
        if (!TOAST_METHODS.has(memberPropertyName(node.callee))) return

        const text = staticTextExpression(node.arguments[0])
        if (text === null) return
        if (objectHasStatusMetadata(node.arguments[1])) return

        context.report({ node, messageId: 'missingMetadata' })
      },
    }
  },
}

module.exports = {
  rules: {
    'no-raw-semantic-identifiers': noRawSemanticIdentifiers,
    'no-unsafe-semantic-cast': noUnsafeSemanticCast,
    'no-localized-prose-assertions': noLocalizedProseAssertions,
    'require-semantic-status-metadata': requireSemanticStatusMetadata,
  },
}
