/** @module Navigation @description Maps SessionSift workspace pages and session views to History API routes without full-page reloads. */
export type Page="sessions"|"analytics"|"compare"|"connections"|"diagnostics";
export type SessionView="conversation"|"timeline"|"tools"|"review";
export type Route={page:Page;sessionId?:string;view?:SessionView};

export const sessionViews:readonly SessionView[]=["conversation","timeline","tools","review"];

const pagePaths:Record<Exclude<Page,"sessions">,string>={analytics:"/analytics",compare:"/compare",connections:"/connections",diagnostics:"/diagnostics"};
// Earlier builds linked these; they now open the connection workspace.
const legacyConnectionPaths=new Set(["/remote-access","/remote-review"]);

export function readRoute(pathname=window.location.pathname):Route{
  const match=pathname.match(/^\/sessions\/([^/]+)(?:\/([a-z]+))?\/?$/);
  if(match){
    const view=sessionViews.find(value=>value===match[2]);
    return view&&view!=="conversation"?{page:"sessions",sessionId:decodeURIComponent(match[1]),view}:{page:"sessions",sessionId:decodeURIComponent(match[1])};
  }
  if(legacyConnectionPaths.has(pathname))return{page:"connections"};
  const page=(Object.keys(pagePaths) as Exclude<Page,"sessions">[]).find(key=>pagePaths[key]===pathname);
  return{page:page||"sessions"};
}

export function routePath(route:Route){
  if(route.page!=="sessions")return pagePaths[route.page];
  if(!route.sessionId)return "/";
  const base=`/sessions/${encodeURIComponent(route.sessionId)}`;
  return route.view&&route.view!=="conversation"?`${base}/${route.view}`:base;
}

export function pushRoute(route:Route){
  const path=routePath(route);
  if(window.location.pathname!==path)window.history.pushState({},"",path);
}

export function replaceRoute(route:Route){
  const path=routePath(route);
  if(window.location.pathname!==path)window.history.replaceState({},"",path);
}
