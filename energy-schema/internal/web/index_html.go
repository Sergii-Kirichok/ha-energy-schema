package web

// indexHTML auto-reloads the given SVG file every refresh seconds.
// %s = svg filename, %d = refresh seconds.
const indexHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;height:100%%;overflow:hidden;background:#0f1115}#c{position:fixed;inset:0}#c svg{width:100%%;height:100%%;display:block}#vo{position:fixed;top:8px;right:12px;background:#1f2937;color:#9ca3af;font:12px sans-serif;padding:4px 10px;border-radius:8px;opacity:.85;z-index:9}
.face{transform-box:fill-box;transform-origin:center;transition:transform .22s ease-in-out}
.f-back{transform:scaleX(0);pointer-events:none}
.flipped>.f-front{transform:scaleX(0);pointer-events:none}
.flipped>.f-back{transform:scaleX(1);pointer-events:auto}
.noanim .face{transition:none}</style></head><body><div id="c"></div><div id="vo" style="display:none">только просмотр</div><script>
var CANCTL=%s;
function ask(act,val){
 var m='Выполнить действие?';
 if(act==='avr_src'){m='Переключить питание Дома на: '+(val==='reserve'?'Резерв (стабилизаторы)':'Инвертор')+'?';}
 if(act==='contactor'){m='Переключить АВР вводов на: '+(val==='in2'?'Ввод 2 (Зелёный)':'Ввод 1 (Рыбхоз)')+'?';}
 if(act==='gen_start'){m='Запустить генератор?';}
 if(act==='gen_stop'){m='Остановить генератор?';}
 if(act==='gen_heater'){m=(val==='on'?'Включить':'Выключить')+' подогрев генератора?';}
 if(!confirm(m))return;
 fetch('control?act='+act+'&val='+val,{method:'POST'}).then(function(r){
  if(!r.ok){r.text().then(function(t){alert('Не выполнено: '+t);});}else{setTimeout(load,400);}
 }).catch(function(e){alert('Ошибка связи: '+e);});
}
function setp(v){fetch('control?act=param&val='+encodeURIComponent(v),{method:'POST'}).then(function(r){
  if(!r.ok){r.text().then(function(t){alert('Не выполнено: '+t);});}else{setTimeout(load,300);}
 }).catch(function(e){alert('Ошибка связи: '+e);});}
function flips(){var g=document.getElementById('card-batt');if(!g)return;
 if(localStorage.getItem('flip_batt')==='1'){g.classList.add('noanim','flipped');setTimeout(function(){g.classList.remove('noanim');},60);}
 var els=document.querySelectorAll('#c [data-flip]');for(var i=0;i<els.length;i++){els[i].addEventListener('click',function(){var on=!g.classList.contains('flipped');g.classList.toggle('flipped',on);localStorage.setItem('flip_batt',on?'1':'0');});}}
function wire(){flips();if(!CANCTL)return;var els=document.querySelectorAll('#c [data-act]');for(var i=0;i<els.length;i++){(function(el){el.addEventListener('click',function(){ask(el.getAttribute('data-act'),el.getAttribute('data-val'));});})(els[i]);}
 var ss=document.querySelectorAll('#c [data-set]');for(var j=0;j<ss.length;j++){(function(el){el.addEventListener('click',function(){setp(el.getAttribute('data-set'));});})(ss[j]);}}
var T0=Date.now();
function load(){fetch('%s?t='+Date.now()).then(function(r){return r.text()}).then(function(t){var c=document.getElementById('c');c.innerHTML=t;var s=c.querySelector('svg');if(s&&s.setCurrentTime){try{s.setCurrentTime((Date.now()-T0)/1000);}catch(e){}}wire();})}
if(!CANCTL){document.getElementById('vo').style.display='block';}
load();setInterval(load,%d000);</script></body></html>`
