import { browserRoutePathForWikiNode } from '@/lib/wikiPath'
import { useTreeStore } from '@/stores/tree'
import { Navigate, useLocation } from 'react-router-dom'

export default function RootRedirect() {
  const location = useLocation()
  const { tree } = useTreeStore()

  if (!tree || !tree.children || tree.children.length === 0) return null

  const first = tree.children[0]
  return (
    <Navigate
      to={browserRoutePathForWikiNode(first.path, first.kind)}
      replace
      state={location.state}
    />
  )
}
