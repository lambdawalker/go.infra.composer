// Run with node --test reporting/testdata/sort.test.cjs (no dependencies).
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),test=require('node:test');
test('sorts siblings without flattening children and preserves disclosure state',()=>{
 const html=fs.readFileSync(require('node:path').join(__dirname,'../report.html'),'utf8');
 const script=html.match(/<script type="text\/javascript">([\s\S]*?)<\/script>/)[1];
 const control=()=>({events:{},addEventListener(n,f){this.events[n]=f},setAttribute(n,v){this[n]=v}});
 const ids={sort:{...control(),value:'priority'},direction:control(),expand:control(),collapse:control()};
 const card=(key,priority,name)=>({dataset:{key,priority:String(priority),name,owner:name,type:name,status:name,feature:name},classList:{contains:n=>n==='artifact'},open:false});
 const a=card('a',30,'Zulu'),b=card('b',10,'Alpha'),child=card('child',1,'Child');a.open=true;
 const list=children=>({children,appendChild(c){this.children=this.children.filter(x=>x!==c);this.children.push(c)}});
 const parents=list([a,b]),children=list([child]);
 vm.runInNewContext(script,{document:{getElementById:id=>ids[id],querySelectorAll:s=>s==='.artifact-list'?[parents,children]:[a,b,child]}});
 ids.sort.events.change();assert.deepEqual(parents.children,[b,a]);assert.deepEqual(children.children,[child]);assert.equal(a.open,true);
 for(const field of ['owner','type','name','status','feature']){ids.sort.value=field;ids.sort.events.change();assert.deepEqual(parents.children,[b,a]);}
 ids.direction.events.click();assert.deepEqual(parents.children,[a,b]);assert.equal(ids.direction['aria-pressed'],'true');
 ids.expand.events.click();assert.ok([a,b,child].every(c=>c.open));ids.collapse.events.click();assert.ok([a,b,child].every(c=>!c.open));
});
