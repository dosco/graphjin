"use strict";(self.webpackChunkweb_ui=self.webpackChunkweb_ui||[]).push([[271],{4497:function(e,n,o){o.d(n,{r:function(){return l}});var t,r=o(8308),i=Object.defineProperty,u=function(e,n){return i(e,"name",{value:n,configurable:!0})},a={exports:{}};function l(){return t||(t=1,function(e){function n(n,o,t){var r,i=n.getWrapperElement();return(r=i.appendChild(document.createElement("div"))).className=t?"CodeMirror-dialog CodeMirror-dialog-bottom":"CodeMirror-dialog CodeMirror-dialog-top","string"==typeof o?r.innerHTML=o:r.appendChild(o),e.addClass(i,"dialog-opened"),r}function o(e,n){e.state.currentNotificationClose&&e.state.currentNotificationClose(),e.state.currentNotificationClose=n}u(n,"dialogDiv"),u(o,"closeNotification"),e.defineExtension("openDialog",(function(t,r,i){i||(i={}),o(this,null);var a=n(this,t,i.bottom),l=!1,s=this;function c(n){if("string"==typeof n)p.value=n;else{if(l)return;l=!0,e.rmClass(a.parentNode,"dialog-opened"),a.parentNode.removeChild(a),s.focus(),i.onClose&&i.onClose(a)}}u(c,"close");var f,p=a.getElementsByTagName("input")[0];return p?(p.focus(),i.value&&(p.value=i.value,!1!==i.selectValueOnOpen&&p.select()),i.onInput&&e.on(p,"input",(function(e){i.onInput(e,p.value,c)})),i.onKeyUp&&e.on(p,"keyup",(function(e){i.onKeyUp(e,p.value,c)})),e.on(p,"keydown",(function(n){i&&i.onKeyDown&&i.onKeyDown(n,p.value,c)||((27==n.keyCode||!1!==i.closeOnEnter&&13==n.keyCode)&&(p.blur(),e.e_stop(n),c()),13==n.keyCode&&r(p.value,n))})),!1!==i.closeOnBlur&&e.on(a,"focusout",(function(e){null!==e.relatedTarget&&c()}))):(f=a.getElementsByTagName("button")[0])&&(e.on(f,"click",(function(){c(),s.focus()})),!1!==i.closeOnBlur&&e.on(f,"blur",c),f.focus()),c})),e.defineExtension("openConfirm",(function(t,r,i){o(this,null);var a=n(this,t,i&&i.bottom),l=a.getElementsByTagName("button"),s=!1,c=this,f=1;function p(){s||(s=!0,e.rmClass(a.parentNode,"dialog-opened"),a.parentNode.removeChild(a),c.focus())}u(p,"close"),l[0].focus();for(var d=0;d<l.length;++d){var m=l[d];(function(n){e.on(m,"click",(function(o){e.e_preventDefault(o),p(),n&&n(c)}))})(r[d]),e.on(m,"blur",(function(){--f,setTimeout((function(){f<=0&&p()}),200)})),e.on(m,"focus",(function(){++f}))}})),e.defineExtension("openNotification",(function(t,r){o(this,c);var i,a=n(this,t,r&&r.bottom),l=!1,s=r&&typeof r.duration<"u"?r.duration:5e3;function c(){l||(l=!0,clearTimeout(i),e.rmClass(a.parentNode,"dialog-opened"),a.parentNode.removeChild(a))}return u(c,"close"),e.on(a,"click",(function(n){e.e_preventDefault(n),c()})),s&&(i=setTimeout(c,s)),c}))}((0,r.r)())),a.exports}u(l,"requireDialog")},3271:function(e,n,o){o.r(n),o.d(n,{j:function(){return s}});var t=o(8308),r=o(4497),i=Object.defineProperty,u=function(e,n){return i(e,"name",{value:n,configurable:!0})};function a(e,n){for(var o=function(){var o=n[t];if("string"!=typeof o&&!Array.isArray(o)){var r=function(n){if("default"!==n&&!(n in e)){var t=Object.getOwnPropertyDescriptor(o,n);t&&Object.defineProperty(e,n,t.get?t:{enumerable:!0,get:function(){return o[n]}})}};for(var i in o)r(i)}},t=0;t<n.length;t++)o();return Object.freeze(Object.defineProperty(e,Symbol.toStringTag,{value:"Module"}))}u(a,"_mergeNamespaces");!function(e){function n(e,n,o,t,r){e.openDialog?e.openDialog(n,r,{value:t,selectValueOnOpen:!0,bottom:e.options.search.bottom}):r(prompt(o,t))}function o(e){return e.phrase("Jump to line:")+' <input type="text" style="width: 10em" class="CodeMirror-search-field"/> <span style="color: #888" class="CodeMirror-search-hint">'+e.phrase("(Use line:column or scroll% syntax)")+"</span>"}function t(e,n){var o=Number(n);return/^[-+]/.test(n)?e.getCursor().line+o:o-1}e.defineOption("search",{bottom:!1}),u(n,"dialog"),u(o,"getJumpDialog"),u(t,"interpretLine"),e.commands.jumpToLine=function(e){var r=e.getCursor();n(e,o(e),e.phrase("Jump to line:"),r.line+1+":"+r.ch,(function(n){var o;if(n)if(o=/^\s*([\+\-]?\d+)\s*\:\s*(\d+)\s*$/.exec(n))e.setCursor(t(e,o[1]),Number(o[2]));else if(o=/^\s*([\+\-]?\d+(\.\d+)?)\%\s*/.exec(n)){var i=Math.round(e.lineCount()*Number(o[1])/100);/^[-+]/.test(o[1])&&(i=r.line+i+1),e.setCursor(i-1,r.ch)}else(o=/^\s*\:?\s*([\+\-]?\d+)\s*/.exec(n))&&e.setCursor(t(e,o[1]),r.ch)}))},e.keyMap.default["Alt-G"]="jumpToLine"}((0,t.r)(),(0,r.r)());var l={},s=a({__proto__:null,default:(0,t.g)(l)},[l])}}]);
//# sourceMappingURL=271.1505a6a8.chunk.js.map