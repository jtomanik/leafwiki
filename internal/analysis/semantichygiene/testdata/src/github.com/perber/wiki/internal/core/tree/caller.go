package tree

func badMarkdownPathCast(raw string) MarkdownPath {
	return MarkdownPath(raw) // want "direct cast to semantic type MarkdownPath outside parser or boundary; use a parser or typed input"
}

func badRouteToMarkdownPath(routePath RoutePath) MarkdownPath {
	return MarkdownPath(string(routePath) + ".md") // want "semantic value RoutePath converted to string before internal call MarkdownPath; make the callee accept RoutePath"
}
