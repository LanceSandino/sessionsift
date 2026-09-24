/** @module Theme @description Resolves and remembers the viewer's light or dark preference without depending on browser storage. */
export type Theme="light"|"dark";

const storageKey="sessionsift.theme";

/** A stored choice wins; otherwise the operating system preference applies. */
export function initialTheme(stored:string|null,prefersDark:boolean):Theme{
  if(stored==="light"||stored==="dark")return stored;
  return prefersDark?"dark":"light";
}

export function readStoredTheme():string|null{
  try{return window.localStorage.getItem(storageKey)}catch{return null}
}

export function storeTheme(theme:Theme){
  try{window.localStorage.setItem(storageKey,theme)}catch{/* Private windows may refuse storage; the choice still applies to this page. */}
}

export function systemPrefersDark():boolean{
  return typeof window.matchMedia==="function"&&window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function applyTheme(theme:Theme,root:HTMLElement=document.documentElement){
  root.dataset.theme=theme;
}

export function nextTheme(theme:Theme):Theme{
  return theme==="dark"?"light":"dark";
}
