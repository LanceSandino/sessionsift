/** @module HostedRichContent @description Reuses the safe React formatter in authenticated hosted surfaces. */
import React from "react";
import {createRoot,type Root} from "react-dom/client";
import {RichContent} from "./components/RichContent";
import styles from "./rich-content.css?inline";
import layout from "./hosted-layout.css?inline";

const style=document.createElement("style");style.textContent=styles+layout;document.head.append(style);
const roots=new Map<HTMLElement,Root>();
let detailPinned=true;
const detail=document.querySelector<HTMLElement>("#sessionDetail");
detail?.addEventListener("scroll",()=>{detailPinned=detail.scrollHeight-detail.scrollTop-detail.clientHeight<48},{passive:true});
function enhance(){
 for(const[element,root]of roots){if(!element.isConnected){root.unmount();roots.delete(element)}}
 document.querySelectorAll<HTMLElement>("[data-rich]:not([data-rich-ready])").forEach(element=>{
  const text=element.textContent||"";element.dataset.richReady="true";
  const root=createRoot(element);roots.set(element,root);root.render(<RichContent text={text}/>);
 });
 document.querySelectorAll<HTMLElement>("[data-card]:not([data-expand-ready]),main>.panel:not([data-expand-ready])").forEach(pane=>{
  pane.dataset.expandReady="true";const button=document.createElement("button");button.className="paneExpand";button.textContent="Expand pane";button.setAttribute("aria-expanded","false");
  button.onclick=()=>{const expanded=pane.classList.toggle("paneExpanded");button.textContent=expanded?"Restore pane":"Expand pane";button.setAttribute("aria-expanded",String(expanded))};pane.prepend(button);
 });
 if(detail&&detailPinned)detail.scrollTop=detail.scrollHeight;
}
document.addEventListener("keydown",event=>{if(event.key==="Escape")document.querySelectorAll<HTMLButtonElement>(".paneExpanded>.paneExpand").forEach(button=>button.click())});
enhance();new MutationObserver(enhance).observe(document.body,{childList:true,subtree:true});
