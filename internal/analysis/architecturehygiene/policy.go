package architecturehygiene

import "github.com/perber/wiki/internal/analysis/checkerpolicy"

const ruleDependencyE2EProxyInternalImport checkerpolicy.RuleID = "dependency.e2e-proxy-internal-import"
const ruleArchitectureCoreToWiki checkerpolicy.RuleID = "architecture.import-boundary.core-to-wiki"
const ruleArchitectureCoreToHTTP checkerpolicy.RuleID = "architecture.import-boundary.core-to-http"
const ruleArchitectureCoreToProjectDaemon checkerpolicy.RuleID = "architecture.import-boundary.core-to-projectdaemon"
const ruleArchitectureWikiToCmdLeafwiki checkerpolicy.RuleID = "architecture.import-boundary.wiki-to-cmd-leafwiki"
const ruleArchitectureProjectDaemonToCmdLeafwiki checkerpolicy.RuleID = "architecture.import-boundary.projectdaemon-to-cmd-leafwiki"
const ruleArchitectureWorkspacedToFrontd checkerpolicy.RuleID = "architecture.import-boundary.workspaced-to-frontd"

var ruleSet = checkerpolicy.NewRuleSet(map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{
	ruleDependencyE2EProxyInternalImport:       checkerpolicy.HardRule(ruleDependencyE2EProxyInternalImport),
	ruleArchitectureCoreToWiki:                 checkerpolicy.HardRule(ruleArchitectureCoreToWiki),
	ruleArchitectureCoreToHTTP:                 checkerpolicy.HardRule(ruleArchitectureCoreToHTTP),
	ruleArchitectureCoreToProjectDaemon:        checkerpolicy.HardRule(ruleArchitectureCoreToProjectDaemon),
	ruleArchitectureWikiToCmdLeafwiki:          checkerpolicy.HardRule(ruleArchitectureWikiToCmdLeafwiki),
	ruleArchitectureProjectDaemonToCmdLeafwiki: checkerpolicy.HardRule(ruleArchitectureProjectDaemonToCmdLeafwiki),
	ruleArchitectureWorkspacedToFrontd:         checkerpolicy.HardRule(ruleArchitectureWorkspacedToFrontd),
}, nil, 0)
