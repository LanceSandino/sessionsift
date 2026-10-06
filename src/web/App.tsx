/** @module App @description The workspace shell: loads local sessions, owns routing, theme, focus and inspector state, and composes the rail, library, stage and dialogs. */
import React,{useCallback,useEffect,useMemo,useRef,useState} from "react";
import {api} from "./api";
import {AppRail} from "./components/AppRail";
import {Analytics} from "./components/Analytics";
import {CommandPalette,type Command} from "./components/CommandPalette";
import {Changelog} from "./components/Changelog";
import {Compare} from "./components/Compare";
import {Connections} from "./components/Connections";
import {Diagnostics} from "./components/Diagnostics";
import {Icon} from "./components/Icon";
import {ImportDialog} from "./components/ImportDialog";
import {Optimize} from "./components/Optimize";
import {SessionNavigator} from "./components/SessionNavigator";
import {SessionWorkspace} from "./components/SessionWorkspace";
import {ShareDialog} from "./components/ShareDialog";
import {StatusBar} from "./components/StatusBar";
import {TopBar,type SupportIndicator} from "./components/TopBar";
import {pushRoute,readRoute,type Page,type SessionView} from "./navigation";
import {sortSessions} from "./components/sessionSort";
import {sessionRevision} from "./sessionRevision";
import {sessionPlace} from "./sources";
import {activeSupportFor,liveStatus} from "./supportState";
import {applyTheme,initialTheme,nextTheme,readStoredTheme,storeTheme,systemPrefersDark,type Theme} from "./theme";
import type {AnalyticsReport,DiagnosticsStatus,Session} from "./types";
import {useCollaboration} from "./useCollaboration";
import {matchesMedia,useMediaQuery} from "./useMediaQuery";
import "./theme.css";
import "./shell.css";
import "./conversation.css";
import "./inspector.css";
import "./views.css";
import "./pages.css";
import "./dialogs.css";
import "./rich-content.css";
import "./responsive.css";

type Overlay="none"|"share"|"import"|"command";
// These match the responsive stylesheet: the inspector overlays below 1181px, the library becomes a drawer below 801px.
const narrowQuery="(max-width: 1180px)";
const drawerQuery="(max-width: 800px)";
const pageTitles:Record<Exclude<Page,"sessions">,{title:string;meta:string}>={
  analytics:{title:"Analytics",meta:"Aggregates only · across every source"},
  compare:{title:"Compare",meta:"Two sessions · observed metrics"},
  optimize:{title:"Optimize",meta:"Recommendations · experiments · weekly report"},
  connections:{title:"Connections",meta:"Owner controls · local authority"},
  diagnostics:{title:"Diagnostics",meta:"Local health · this device"},
  changelog:{title:"What's new",meta:"Semantic releases · bounded history"},
};

export function App(){
  const initial=useMemo(()=>readRoute(),[]);
  const[sessions,setSessions]=useState<Session[]>([]);
  const[loaded,setLoaded]=useState(false);
  const[selected,setSelected]=useState(initial.sessionId||"");
  const[detail,setDetail]=useState<Session>();
  const[analytics,setAnalytics]=useState<AnalyticsReport>();
  const[diagnostics,setDiagnostics]=useState<DiagnosticsStatus>();
  const[diagnosticsError,setDiagnosticsError]=useState("");
  const[page,setPage]=useState<Page>(initial.page);
  const[view,setView]=useState<SessionView>(initial.view||"conversation");
  const[error,setError]=useState("");
  const[refreshedAt,setRefreshedAt]=useState<string>();
  const[theme,setTheme]=useState<Theme>(()=>initialTheme(readStoredTheme(),systemPrefersDark()));
  const[focus,setFocus]=useState(false);
  const[inspectorHidden,setInspectorHidden]=useState(false);
  const[inspectorOverlay,setInspectorOverlay]=useState(false);
  const[sidebarOpen,setSidebarOpen]=useState(false);
  const[overlay,setOverlay]=useState<Overlay>("none");
  const[toast,setToast]=useState("");
  const[support,setSupport]=useState<SupportIndicator>("none");
  const[now,setNow]=useState(()=>Date.now());
  const searchRef=useRef<HTMLInputElement>(null);
  const narrow=useMediaQuery(narrowQuery);
  const selectedSummary=sessions.find(item=>item.id===selected);
  const selectedRevision=sessionRevision(selectedSummary);
  const collaboration=useCollaboration(page==="sessions"&&detail?detail.id:undefined);
  const visibleSessions=useMemo(()=>sessions.filter(item=>item.session_kind!=="internal_review"),[sessions]);

  useEffect(()=>applyTheme(theme),[theme]);
  useEffect(()=>{const timer=window.setInterval(()=>setNow(Date.now()),60000);return()=>window.clearInterval(timer)},[]);
  useEffect(()=>{const onPop=()=>{const route=readRoute();setPage(route.page);setView(route.view||"conversation");if(route.sessionId)setSelected(route.sessionId)};window.addEventListener("popstate",onPop);return()=>window.removeEventListener("popstate",onPop)},[]);
  useEffect(()=>{let active=true;let timer=0;const refresh=()=>api.sessions(true).then(items=>{if(!active)return;setSessions(items);setLoaded(true);setRefreshedAt(new Date().toISOString());setSelected(value=>value||items.find(item=>item.session_kind!=="internal_review")?.id||items[0]?.id||"");setError("")}).catch(err=>active&&setError(String(err))).finally(()=>{if(active)timer=window.setTimeout(refresh,5000)});refresh();return()=>{active=false;window.clearTimeout(timer)}},[]);
  useEffect(()=>{setDetail(undefined)},[selected]);
  useEffect(()=>{if(!selected)return;let active=true;api.session(selected).then(session=>{if(active){setDetail(session);setError("")}}).catch(err=>active&&setError(String(err)));return()=>{active=false}},[selected,selectedRevision]);
  useEffect(()=>{if(page!=="analytics")return;let active=true;api.analytics().then(value=>{if(active){setAnalytics(value);setError("")}}).catch(err=>active&&setError(String(err)));return()=>{active=false}},[page]);
  // Collector activity is live only while Diagnostics is open; a failed probe stays on that page.
  useEffect(()=>{if(page!=="diagnostics")return;let active=true;let timer=0;const refresh=()=>api.diagnostics().then(value=>{if(active){setDiagnostics(value);setDiagnosticsError("")}}).catch(err=>active&&setDiagnosticsError(String(err))).finally(()=>{if(active)timer=window.setTimeout(refresh,5000)});refresh();return()=>{active=false;window.clearTimeout(timer)}},[page]);
  useEffect(()=>{
    if(!selected){setSupport("none");return}
    let active=true;
    const check=()=>api.supportSessions().then(values=>{if(!active)return;const current=activeSupportFor(values,selected,Date.now());setSupport(current?liveStatus(current,Date.now())==="connected"?"connected":"waiting":"none")}).catch(()=>{if(active)setSupport("none")});
    check();const timer=window.setInterval(check,10000);
    return()=>{active=false;window.clearInterval(timer)};
  },[selected,overlay]);
  useEffect(()=>{if(!toast)return;const timer=window.setTimeout(()=>setToast(""),3800);return()=>window.clearTimeout(timer)},[toast]);

  const closeSurfaces=()=>{setSidebarOpen(false);setInspectorOverlay(false)};
  const selectSession=useCallback((id:string)=>{setSelected(id);setPage("sessions");setView("conversation");setSidebarOpen(false);setInspectorOverlay(false);pushRoute({page:"sessions",sessionId:id})},[]);
  const changePage=useCallback((next:Page)=>{setPage(next);setFocus(false);setSidebarOpen(false);setInspectorOverlay(false);pushRoute({page:next,sessionId:next==="sessions"?selected||undefined:undefined})},[selected]);
  const changeView=useCallback((next:SessionView)=>{setPage("sessions");setView(next);pushRoute({page:"sessions",sessionId:selected||undefined,view:next})},[selected]);
  const toggleTheme=useCallback(()=>setTheme(value=>{const next=nextTheme(value);storeTheme(next);return next}),[]);
  const toggleFocus=useCallback(()=>{setFocus(value=>!value);closeSurfaces()},[]);
  // Layout questions are answered at action time: the stored breakpoint can lag a resize.
  const revealInspector=useCallback(()=>{setFocus(false);setInspectorHidden(false);if(matchesMedia(narrowQuery))setInspectorOverlay(true)},[]);
  const dismissOverlay=useCallback(()=>setInspectorOverlay(false),[]);
  const closeInspector=useCallback(()=>{setInspectorHidden(true);setInspectorOverlay(false)},[]);
  const toggleInspector=useCallback(()=>{
    const overlayLayout=matchesMedia(narrowQuery);
    if(focus){setFocus(false);setInspectorHidden(false);if(overlayLayout)setInspectorOverlay(true);return}
    if(overlayLayout){setInspectorHidden(false);setInspectorOverlay(value=>!value);return}
    setInspectorHidden(value=>!value);
  },[focus]);
  const notify=useCallback((text:string)=>setToast(text),[]);
  const openCompare=useCallback(()=>changePage("compare"),[changePage]);

  useEffect(()=>{
    const onKey=(event:KeyboardEvent)=>{
      const target=event.target;
      const typing=target instanceof Element&&Boolean(target.closest("input,textarea,select,[contenteditable=true]"));
      const modifier=event.ctrlKey||event.metaKey;
      if(modifier&&event.key.toLowerCase()==="k"){event.preventDefault();setOverlay(value=>value==="command"?"none":"command");return}
      if(overlay!=="none")return;
      if(modifier&&event.shiftKey&&event.key.toLowerCase()==="f"){if(page==="sessions"){event.preventDefault();toggleFocus()}return}
      if(event.key==="/"&&!typing&&!modifier){event.preventDefault();setFocus(false);if(matchesMedia(drawerQuery))setSidebarOpen(true);requestAnimationFrame(()=>searchRef.current?.focus());return}
      if(event.key==="Escape"){if(sidebarOpen||inspectorOverlay)closeSurfaces();else if(focus)setFocus(false)}
    };
    window.addEventListener("keydown",onKey);
    return()=>window.removeEventListener("keydown",onKey);
  },[overlay,page,sidebarOpen,inspectorOverlay,focus,toggleFocus]);

  const commands=useMemo<Command[]>(()=>{
    // Sessions follow the library's default order, most recently updated first.
    const list:Command[]=sortSessions(visibleSessions,"updated_desc").map(session=>({id:`session:${session.id}`,label:session.title,icon:"chat",kind:"session",run:()=>selectSession(session.id)}));
    if(detail)list.push(
      {id:"view:conversation",label:"Conversation",icon:"chat",kind:"view",run:()=>changeView("conversation")},
      {id:"view:timeline",label:"Timeline",icon:"timeline",kind:"view",run:()=>changeView("timeline")},
      {id:"view:tools",label:"Tools & evidence",icon:"tool",kind:"view",run:()=>changeView("tools")},
      {id:"view:review",label:"Review notes & decision",icon:"note",kind:"view",run:()=>changeView("review")},
      {id:"action:share",label:"Share this conversation",icon:"users",kind:"action",run:()=>{setPage("sessions");setOverlay("share")}},
      {id:"action:focus",label:"Toggle focus mode",icon:"focus",kind:"action",run:()=>{setPage("sessions");toggleFocus()}},
      {id:"action:inspector",label:"Toggle inspector",icon:"panel",kind:"action",run:toggleInspector},
    );
    list.push(
      {id:"action:theme",label:"Toggle light / dark theme",icon:theme==="dark"?"sun":"moon",kind:"action",run:toggleTheme},
      {id:"action:import",label:"Import conversations",icon:"import",kind:"action",run:()=>setOverlay("import")},
      {id:"page:analytics",label:"Analytics",icon:"chart",kind:"page",run:()=>changePage("analytics")},
      {id:"page:compare",label:"Compare sessions",icon:"compare",kind:"page",run:()=>changePage("compare")},
      {id:"page:optimize",label:"Optimize workflow",icon:"spark",kind:"page",run:()=>changePage("optimize")},
      {id:"page:connections",label:"Connections",icon:"link",kind:"page",run:()=>changePage("connections")},
      {id:"page:diagnostics",label:"Diagnostics",icon:"health",kind:"page",run:()=>changePage("diagnostics")},
      {id:"page:changelog",label:"Changelog",icon:"file",kind:"page",run:()=>changePage("changelog")},
    );
    return list;
  },[visibleSessions,detail,theme,selectSession,changeView,changePage,toggleFocus,toggleInspector,toggleTheme]);

  const sessionPage=page==="sessions";
  const inspectorOpen=narrow?inspectorOverlay:!inspectorHidden&&!focus;
  const appClass=["app",focus&&sessionPage?"focus-mode":"",inspectorHidden?"inspector-hidden":"",inspectorOverlay?"inspector-overlay":"",sidebarOpen?"sidebar-open":""].filter(Boolean).join(" ");
  const location=sessionPage?`Local library / ${visibleSessions.length} sessions`:pageTitles[page].title;

  let stage:React.ReactNode;
  if(error)stage=<Pane><Message tone="error" icon="warning" title="Local API unavailable" body={error}/></Pane>;
  else if(sessionPage){
    if(detail?.metrics)stage=<SessionWorkspace key={detail.id} session={detail} view={view} collaboration={collaboration} onView={changeView} onRevealInspector={revealInspector} onDismissOverlay={dismissOverlay} onCloseInspector={closeInspector} onCompare={openCompare} onToast={notify}/>;
    else if(loaded&&!sessions.length)stage=<Pane><Message icon="chat" title="No conversations yet" body="Codex and Claude Code sessions on this machine appear here automatically; chat exports are imported explicitly." action={<button type="button" className="button primary" onClick={()=>setOverlay("import")}><Icon name="import"/>How to import</button>}/></Pane>;
    else stage=<Pane><Message icon="clock" title={selected?"Loading conversation…":"Importing local conversations…"} body="Transcripts are read and redacted on this device."/></Pane>;
  }else{
    const {title,meta}=pageTitles[page];
    stage=<>
      <div className="session-header"><div className="grow"><h1>{title}</h1><div className="metadata">{meta}</div></div></div>
      <div className="workbench"><section className="main-pane" aria-label={title}><div className="main-scroll" tabIndex={0}>
        {page==="analytics"&&(analytics?<Analytics report={analytics}/>:<Message icon="chart" title="Calculating local aggregates…" body="Only counts and totals are computed; no conversation text is read for this page."/>)}
        {page==="compare"&&<Compare sessions={visibleSessions} initial={visibleSessions.some(item=>item.id===selected)?selected:visibleSessions[0]?.id}/>}
        {page==="optimize"&&<Optimize sessions={visibleSessions}/>}
        {page==="connections"&&<Connections/>}
        {page==="diagnostics"&&<Diagnostics sessions={sessions} status={diagnostics} statusError={diagnosticsError}/>}
        {page==="changelog"&&<Changelog/>}
      </div></section></div>
    </>;
  }

  return <>
    <div className={appClass}>
      <div className="mobile-scrim" aria-hidden="true" onClick={closeSurfaces}/>
      <AppRail page={page} theme={theme} onPage={changePage} onTheme={toggleTheme}/>
      <SessionNavigator sessions={sessions} selected={sessionPage?selected:undefined} page={page} now={now} searchRef={searchRef} onSelect={selectSession} onPage={changePage} onImport={()=>setOverlay("import")} onClose={()=>setSidebarOpen(false)}/>
      <main className="stage" aria-label={sessionPage?"Session":pageTitles[page].title}>
        <TopBar page={page} place={detail?sessionPlace(detail):undefined} focus={focus} inspectorOpen={inspectorOpen} support={support} canShare={Boolean(detail)} onMenu={()=>setSidebarOpen(true)} onCommand={()=>setOverlay("command")} onFocus={toggleFocus} onInspector={toggleInspector} onSupport={()=>setOverlay("share")}/>
        {stage}
      </main>
      <StatusBar online={!error} location={location} refreshedAt={refreshedAt} onDiagnostics={()=>changePage("diagnostics")}/>
    </div>
    <ShareDialog open={overlay==="share"} session={detail} onClose={()=>setOverlay("none")} onOpenConnections={()=>{setOverlay("none");changePage("connections")}}/>
    <ImportDialog open={overlay==="import"} onClose={()=>setOverlay("none")}/>
    <CommandPalette open={overlay==="command"} commands={commands} onClose={()=>setOverlay("none")}/>
    <div className={`toast ${toast?"":"hidden"}`} role="status" aria-live="polite">{toast}</div>
  </>;
}

function Pane({children}:{children:React.ReactNode}){
  return <div className="workbench"><div className="main-scroll">{children}</div></div>;
}

function Message({icon,title,body,tone,action}:{icon:React.ComponentProps<typeof Icon>["name"];title:string;body:string;tone?:"error";action?:React.ReactNode}){
  return <div className={`state-message ${tone||""}`} role={tone==="error"?"alert":"status"}><Icon name={icon}/><h2>{title}</h2><p>{body}</p>{action&&<p>{action}</p>}</div>;
}
