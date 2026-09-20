/** @module Navigation @description Maps SessionSift screens to History API routes without full-page reloads. */
import type {View} from "./components/Sidebar";

export type Route={view:View;sessionId?:string};

export function readRoute(pathname=window.location.pathname):Route{
  const match=pathname.match(/^\/sessions\/([^/]+)$/);
  if(match)return{view:"sessions",sessionId:decodeURIComponent(match[1])};
  if(pathname==="/compare")return{view:"compare"};
  if(pathname==="/diagnostics")return{view:"diagnostics"};
  return{view:"sessions"};
}

export function routePath(route:Route){
  if(route.view==="compare")return "/compare";
  if(route.view==="diagnostics")return "/diagnostics";
  return route.sessionId?`/sessions/${encodeURIComponent(route.sessionId)}`:"/";
}

export function pushRoute(route:Route){
  const path=routePath(route);
  if(window.location.pathname!==path)window.history.pushState({},"",path);
}

export function replaceRoute(route:Route){
  window.history.replaceState({},"",routePath(route));
}
