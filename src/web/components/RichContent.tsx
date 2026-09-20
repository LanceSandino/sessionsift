/** @module RichContent @description Presents untrusted Markdown and JSON as inert, bounded React nodes. */
import React,{useMemo,useState} from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

const previewLimit=24000;
const displayLimit=100000;
// No relative, protocol-relative, file, data, script, or credential-bearing URLs.
export function safeContentURL(value:string):string {
 try {const url=new URL(value);return url.protocol==="https:"&&!url.username&&!url.password?url.href:""} catch {return ""}
}

export function RichContent({text}:{text:string}){
 const[expanded,setExpanded]=useState(false);
 const[source,setSource]=useState(false);
 const visible=text.slice(0,expanded?displayLimit:previewLimit);
 const json=useMemo(()=>{
  if(!/^[\s]*[\[{]/.test(visible))return null;
  try{return JSON.stringify(JSON.parse(visible),null,2)}catch{return null}
 },[visible]);
 return <div className="richContent">
  <div className="richToolbar"><span>{source?"Source":json?"JSON":"Formatted"}</span><button type="button" aria-label={source?"Show formatted content":"Show source"} onClick={()=>setSource(!source)}>{source?"Formatted":"Source"}</button></div>
  {source||json?<pre className="richCode"><code>{source?visible:json}</code></pre>:<Markdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={safeContentURL} components={{
   a:({href,children})=>href?<a href={href} target="_blank" rel="noopener noreferrer nofollow">{children}</a>:<span>{children}</span>,
   img:({alt})=><span className="blockedImage">[Image not loaded{alt?`: ${alt}`:""}]</span>,
   table:({children})=><div className="richTable" tabIndex={0} aria-label="Scrollable data table"><table>{children}</table></div>,
  }}>{visible}</Markdown>}
  {!expanded&&text.length>previewLimit&&<button type="button" className="richExpand" onClick={()=>setExpanded(true)}>Show more content</button>}
  {expanded&&text.length>displayLimit&&<small>Display capped at 100,000 characters for responsiveness. Use the original local record for the remainder.</small>}
 </div>
}
