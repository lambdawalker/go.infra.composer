// Run with node --test reporting/testdata/sort.test.cjs (no dependencies).
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),test=require('node:test');
const html=fs.readFileSync(require('node:path').join(__dirname,'../report.html'),'utf8');
const script=html.match(/<script type="text\/javascript">([\s\S]*?)<\/script>/)[1];
function fixture(){
 const control=()=>({value:'',textContent:'',hidden:false,events:{},children:[],addEventListener(n,f){this.events[n]=f},setAttribute(n,v){this[n]=v},appendChild(c){this.children.push(c)}});
 const ids={sort:{...control(),value:'priority'},direction:control(),expand:control(),collapse:control(),search:control(),reset:control(),'result-count':control(),'filter-empty':control()};
 for(const field of ['status','owner','type','feature'])ids['filter-'+field]=control();
 const card=(key,priority,name,extra={})=>({dataset:{key,priority:String(priority),name,owner:'team',type:'bucket',status:'present',feature:'capture',...extra},classList:{contains:n=>n==='artifact'},open:false,hidden:false});
 const a=card('a',30,'Zulu'),b=card('b',10,'Alpha',{owner:'other',feature:'review'}),child=card('child',1,'Needle',{type:'object',status:'unknown'}),grandchild=card('grandchild',1,'Nested',{feature:'review'});
 const list=(children,parent=null)=>{const l={children,appendChild(c){this.children=this.children.filter(x=>x!==c);this.children.push(c)},closest(){return parent}};for(const c of children)c.parentElement=l;return l};
 const parents=list([a,b]),children=list([child],a),grandchildren=list([grandchild],child),cards=[a,b,child,grandchild];
 const selectors=[];
 const context={document:{getElementById:id=>ids[id],createElement:()=>control(),querySelectorAll:s=>{selectors.push(s);return s==='.artifact-list'?[parents,children,grandchildren]:cards}}};
 function run(){vm.runInNewContext(script,context)}
 function filter(field,value){const el=ids['filter-'+field];el.value=el.children.find(o=>o.textContent===value).value;el.events.change()}
 function search(value){ids.search.value=value;ids.search.events.input()}
 const visible=()=>cards.filter(c=>!c.hidden).map(c=>c.dataset.key);
 return {ids,a,b,child,grandchild,parents,children,cards,run,filter,search,visible,selectors};
}
test('sorts siblings without flattening children and preserves disclosure state',()=>{
 const f=fixture(),{ids,a,b,child,parents,children,cards}=f;a.open=true;f.run();
 ids.sort.events.change();assert.deepEqual(parents.children,[b,a]);assert.deepEqual(children.children,[child]);assert.equal(a.open,true);
 for(const field of ['owner','type','name','status','feature']){ids.sort.value=field;ids.sort.events.change();assert.deepEqual(parents.children,['owner','name'].includes(field)?[b,a]:[a,b]);}
 ids.sort.value='name';ids.sort.events.change();
 ids.direction.events.click();assert.deepEqual(parents.children,[a,b]);assert.equal(ids.direction['aria-pressed'],'true');
 ids.expand.events.click();assert.ok(cards.every(c=>c.open));ids.collapse.events.click();assert.ok(cards.every(c=>!c.open));
});
test('child matches retain ancestors; matching parents include their complete subtree',()=>{
 const f=fixture();f.a.open=true;f.run();f.search('needle');
 assert.deepEqual(f.visible(),['a','child','grandchild']);assert.equal(f.ids['result-count'].textContent,'1 of 4 artifacts match · 3 shown with hierarchy');assert.equal(f.a.open,true);assert.equal(f.child.open,false);
 f.search('nested');assert.deepEqual(f.visible(),['a','child','grandchild']);
 f.search('Zulu');assert.deepEqual(f.visible(),['a','child','grandchild']);
 f.search('  aLpHa  ');assert.deepEqual(f.visible(),['b']);
});
test('combines exact filters and search, reports no results, and resets filters only',()=>{
 const f=fixture();f.run();f.filter('status','unknown');f.filter('owner','team');f.filter('type','object');f.filter('feature','capture');f.search('needle');
 assert.deepEqual(f.visible(),['a','child','grandchild']);
 f.filter('owner','other');assert.deepEqual(f.visible(),[]);assert.equal(f.ids['filter-empty'].hidden,false);assert.equal(f.ids['result-count'].textContent,'0 of 4 artifacts match · 0 shown with hierarchy');
 f.ids.sort.value='name';f.ids.direction.events.click();f.a.open=true;f.ids.reset.events.click();
 assert.deepEqual(f.visible(),['a','b','child','grandchild']);assert.equal(f.ids['filter-empty'].hidden,true);assert.equal(f.ids.search.value,'');assert.equal(f.ids.sort.value,'name');assert.equal(f.ids.direction['aria-pressed'],'true');assert.equal(f.a.open,true);
 for(const field of ['status','owner','type','feature'])assert.equal(f.ids['filter-'+field].value,'');
});
test('dropdowns use deterministic unique text options, including missing and hostile metadata',()=>{
 const f=fixture(),hostile='\"><img src=x onerror="globalThis.compromised=true">';
 f.a.dataset.owner=hostile;f.b.dataset.owner='';f.child.dataset.owner='team';f.grandchild.dataset.owner='team';f.run();
 const options=f.ids['filter-owner'].children;assert.deepEqual(options.map(o=>o.textContent),['Not specified',hostile,'team']);assert.ok(options.every(o=>!Object.hasOwn(o,'innerHTML')));
 f.filter('owner',hostile);assert.deepEqual(f.visible(),['a','child','grandchild']);f.filter('owner','Not specified');assert.deepEqual(f.visible(),['b']);
 f.search(hostile);assert.deepEqual(f.visible(),[]);assert.equal(globalThis.compromised,undefined);assert.ok(f.selectors.every(s=>s==='.artifact-list'||s==='details.artifact'));
});

test('context ancestors do not include unmatched siblings and sorting retains filtering',()=>{
 const f=fixture();f.grandchild.parentElement=f.child.parentElement;f.run();f.search('needle');
 assert.deepEqual(f.visible(),['a','child']);
 f.ids.sort.value='name';f.ids.sort.events.change();f.ids.direction.events.click();assert.deepEqual(f.visible(),['a','child']);
 f.search('grandchild');assert.deepEqual(f.visible(),['a','grandchild']);
});
test('empty inventories retain the report empty state without a second no-results message',()=>{
 const f=fixture();f.cards.splice(0);f.run();assert.equal(f.ids['result-count'].textContent,'0 of 0 artifacts match · 0 shown with hierarchy');assert.equal(f.ids['filter-empty'].hidden,true);
});
