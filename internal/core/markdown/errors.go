package markdown

import "errors"

var (
	ErrFrontmatterParse = errors.New("frontmatter parse error")
	ErrNotMarkdownFile  = errors.New("file is not a markdown file")
)
