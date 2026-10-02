// Isolated full-binary/real-browser rehearsal. Never reads merchant secrets.
// Node >=22, Python3, Redis fixture, and Playwright Chromium are prerequisites.
import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { generateKeyPairSync, sign, randomBytes } from 'node:crypto'
import { mkdirSync, writeFileSync, openSync } from 'node:fs'
import { resolve } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
const containerMode=process.env.DIRECTPAY_CONTAINER==='1'
const root=process.cwd(), dir=resolve(root,'.directpay-tools/rehearsal-'+Date.now())
mkdirSync(dir,{recursive:true,mode:0o700})
const db=resolve(dir,'directpay.db'), origin='http://127.0.0.1:18301'
const baseline=resolve(root,'.directpay-tools/new-api-baseline'), old=resolve(root,'.directpay-tools/new-api'), current=resolve(root,'.directpay-tools/new-api-next')
const {privateKey,publicKey}=generateKeyPairSync('rsa',{modulusLength:2048,privateKeyEncoding:{format:'pem',type:'pkcs1'},publicKeyEncoding:{format:'pem',type:'spki'}})
const cfg={deployment_tier:'local',base_url:'https://local.test',create_enabled:true,max_money_minor:100000,allowed_users:[1],accounts:[{current:true,config:{provider:'alipay_direct',environment:'sandbox',account:'fixture',revision:'v1',app_id:'fixture-app',merchant_id:'fixture-merchant',verification_mode:'public_key',private_key:privateKey,public_key:publicKey}}]}
const configPath=resolve(dir,'fixture.json');writeFileSync(configPath,JSON.stringify(cfg),{mode:0o600})
const password=randomBytes(20).toString('hex')+'Aa1!'
const env={...process.env,SQL_DSN:'',LOG_SQL_DSN:'',SQLITE_PATH:db,SESSION_SECRET:randomBytes(32).toString('hex'),SESSION_COOKIE_SECURE:'false',SESSION_COOKIE_TRUSTED_URL:'',REDIS_CONN_STRING:'redis://127.0.0.1:16379/7',BATCH_UPDATE_ENABLED:'false',HTTPS_PROXY:'http://127.0.0.1:1',HTTP_PROXY:'http://127.0.0.1:1',NO_PROXY:'127.0.0.1,localhost',DIRECTPAY_CONFIG_FILE:'',GLOBAL_API_RATE_LIMIT:'10000',CRITICAL_RATE_LIMIT:'10000'}
let children=[],browser
async function launch(binary,port,config=true){
 const log=openSync(resolve(dir,`app-${port}-${Date.now()}.log`),'w')
 const childEnv={...env,DIRECTPAY_CONFIG_FILE:config?configPath:''}
 let proc
 if(containerMode){
  const name='dp-rehearsal-'+port+'-'+Date.now()
  const tag=binary===baseline?'baseline':binary.endsWith('compatible-rollback')?'compatible':'current'
  childEnv.REDIS_CONN_STRING='redis://newapi-directpay-test-redis:6379/7'
  const envKeys=['SQL_DSN','LOG_SQL_DSN','SQLITE_PATH','SESSION_SECRET','SESSION_COOKIE_SECURE','SESSION_COOKIE_TRUSTED_URL','REDIS_CONN_STRING','BATCH_UPDATE_ENABLED','HTTPS_PROXY','HTTP_PROXY','NO_PROXY','DIRECTPAY_CONFIG_FILE','GLOBAL_API_RATE_LIMIT','CRITICAL_RATE_LIMIT']
  proc=spawn('docker',['run','--rm','--user',`${process.getuid()}:${process.getgid()}`,'--name',name,'--network','directpay-rehearsal','-p',`127.0.0.1:${port}:${port}`,'-v',`${dir}:${dir}`,'-w',dir,...envKeys.flatMap(k=>['-e',k+'='+childEnv[k]]),'newapi-directpay-rehearsal:'+tag,'--port',String(port),'--log-dir',dir],{stdio:['ignore',log,log]})
  proc.containerName=name
 }else proc=spawn(binary,['--port',String(port),'--log-dir',dir],{cwd:dir,env:childEnv,stdio:['ignore',log,log]})
 children.push(proc)
 for(let i=0;i<120;i++){if(proc.exitCode!==null)throw Error('app exited '+proc.exitCode+' log at '+dir);try{if((await fetch(`http://127.0.0.1:${port}/api/setup`)).ok)return proc}catch{}await delay(250)}throw Error('startup timeout')
}
async function stop(proc){if(proc.exitCode!==null)return;if(proc.containerName)spawnSync('docker',['stop','-t','5',proc.containerName],{stdio:'ignore'});else proc.kill('SIGTERM');await Promise.race([new Promise(r=>proc.once('exit',r)),delay(10000)]);if(proc.exitCode===null)proc.kill('SIGKILL')}
function sql(statement,args=[]){const p=spawnSync('python3',['-c',`import sqlite3,json,sys\nx=json.load(sys.stdin);c=sqlite3.connect(x['db'],timeout=30);r=c.execute(x['sql'],x['args']);print(json.dumps(r.fetchall()));c.commit()`],{input:JSON.stringify({db,sql:statement,args}),encoding:'utf8'});assert.equal(p.status,0,p.stderr);return JSON.parse(p.stdout)}
async function api(path,method='GET',data,token,port=18301){const r=await fetch(`http://127.0.0.1:${port}${path}`,{method,headers:{'Content-Type':'application/json',...(token?{Authorization:'Bearer '+token}:{})},body:data?JSON.stringify(data):undefined});const v=await r.json();assert.equal(v.success,true,JSON.stringify({path,status:r.status,body:v}));return v.data}
async function purchase(token,key){const quote=await api('/api/user/direct-pay/quote','POST',{amount:1,method:'alipay_page'},token);const r=await fetch(origin+'/api/user/direct-pay/orders',{method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+token,'Idempotency-Key':key},body:JSON.stringify({amount:1,method:'alipay_page',quote:quote.quote})});const v=await r.json();assert.equal(v.success,true,JSON.stringify(v));return v.data}
function notification(order){const f={app_id:'fixture-app',seller_id:'fixture-merchant',out_trade_no:order.order_no,trade_no:'txn-'+order.order_no,trade_status:'TRADE_SUCCESS',total_amount:(order.money_minor/100).toFixed(2),gmt_payment:'2026-10-02 12:00:00',notify_id:'evt-'+order.order_no};const canonical=Object.keys(f).sort().map(k=>k+'='+f[k]).join('&');f.sign=sign('RSA-SHA256',Buffer.from(canonical),privateKey).toString('base64');f.sign_type='RSA2';return new URLSearchParams(f).toString()}
async function notify(order,port=18301){const r=await fetch(`http://127.0.0.1:${port}/api/direct-pay/notify/fixture.v1`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:notification(order)});assert.equal(r.status,200);assert.equal(await r.text(),'success')}
async function credited(order){for(let i=0;i<60;i++){const rows=sql('select settlement_state from direct_pay_orders where order_no=?',[order.order_no]);if(rows[0][0]==='credited')return;await delay(250)}throw Error('credit timeout')}
try{
 assert.equal(spawnSync('docker',['exec','newapi-directpay-test-redis','redis-cli','-n','7','FLUSHDB'],{stdio:'ignore'}).status,0)
 let p=await launch(baseline,18301,false)
 await api('/api/setup','POST',{username:'dpfixture',password,confirmPassword:password})
 assert.equal(sql("select count(*) from sqlite_master where name='direct_pay_orders'")[0][0],0)
 console.log('PASS original 1a4166d8 full schema initialized, directpay tables absent')
 await stop(p)
 for(const [k,v] of Object.entries({'payment_setting.compliance_confirmed':'true','payment_setting.compliance_terms_version':'v1','directpay.create_enabled':'true'}))sql('insert or replace into options(key,value) values(?,?)',[k,v])
 p=await launch(old,18301)
 let login=await api('/api/user/login','POST',{username:'dpfixture',password});let token=login.access_token
 const before=sql('select quota from users where id=1')[0][0]
 const order=await purchase(token,'rehearsal-upgrade-0001')
 const other=await purchase(token,'rehearsal-crash-0002')
 const rollbackOrder=await purchase(token,'rehearsal-rollback-0003')
 const snapshot={minor:order.money_minor,quota:order.quota_to_credit}
 console.log('PASS pre-upgrade actual HTTP orders persisted with immutable snapshots')
 await stop(p)
 sql('insert or replace into options(key,value) values(?,?)',['Price','99'])
 p=await launch(current,18301);const second=await launch(current,18302)
 // Two independently running full servers/pools race durable signed notifications.
 const timing=await Promise.all(Array.from({length:40},async(_,i)=>{const start=performance.now();await notify(order,i%2?18302:18301);return performance.now()-start}))
 await credited(order)
 assert.equal(sql('select count(*) from direct_pay_ledgers where order_id=?',[order.id])[0][0],1)
 assert.equal(sql('select quota from users where id=1')[0][0],before+snapshot.quota)
 assert.equal(sql('select money_minor from direct_pay_orders where id=?',[order.id])[0][0],snapshot.minor)
 console.log('PASS 40 real signed callbacks across two full instances, exactly one ledger, original pricing credit; local ACK max_ms='+Math.max(...timing).toFixed(1))
 // Durable ACK survives abrupt process termination; the other instance settles.
 await notify(other);if(p.containerName)spawnSync('docker',['kill',p.containerName],{stdio:'ignore'});else p.kill('SIGKILL');await credited(other)
 assert.equal(sql('select quota from users where id=1')[0][0],before+2*snapshot.quota)
 console.log('PASS durable Inbox after ACK then SIGKILL, surviving peer settles once')
 await stop(second)
 // Compatible rollback binary is supplied explicitly; stop new orders but retain notify/worker.
 cfg.create_enabled=false;writeFileSync(configPath,JSON.stringify(cfg),{mode:0o600})
 p=await launch(resolve(root,'.directpay-tools/new-api-compatible-rollback'),18301)
 login=await api('/api/user/login','POST',{username:'dpfixture',password});token=login.access_token
 assert.deepEqual(await api('/api/user/direct-pay/methods','GET',undefined,token),[])
 await notify(order);await delay(500)
 assert.equal(sql('select quota from users where id=1')[0][0],before+2*snapshot.quota)
 await notify(rollbackOrder);await credited(rollbackOrder)
 assert.equal(sql('select quota from users where id=1')[0][0],before+3*snapshot.quota)
 console.log('PASS compatible prior binary rollback with creation disabled: late pre-upgrade order settles from original snapshot')
 await stop(p);cfg.create_enabled=true;writeFileSync(configPath,JSON.stringify(cfg),{mode:0o600});p=await launch(current,18301)
 // Browser uses the real embedded production UI and backend authentication.
 const {chromium}=await import(process.env.DIRECTPAY_PLAYWRIGHT||'/opt/codex/runtimes/cua/lib/node_modules/playwright-core/index.mjs')
 browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox']})
 const context=await browser.newContext({locale:'en-US'})
 await context.route('https://**',r=>r.abort()) // never navigate to a payment gateway
 const page=await context.newPage()
 await context.request.post(origin+'/api/user/login',{data:{username:'dpfixture',password}})
 await page.addInitScript(({order})=>{localStorage.setItem('i18nextLng','en');if(!localStorage.getItem('direct-payment-orders-v1')){localStorage.setItem('direct-payment-orders-v1',JSON.stringify({state:{orders:{1:order.order_no},purchases:{}},version:0}))}},{order})
 await page.goto(origin+'/wallet');try { await page.getByText(order.order_no,{exact:true}).waitFor({timeout:15000}) } catch(e) { console.log('browser diagnostic',await page.evaluate(()=>({url:location.pathname,store:localStorage.getItem('direct-payment-orders-v1'),text:document.body.innerText})));throw e }
 assert.match(await page.locator('body').innerText(),/credited/)
 assert.equal(await page.getByRole('button',{name:'Go to Alipay',exact:true}).count(),0)
 await page.reload();await page.getByText(order.order_no,{exact:true}).waitFor()
 assert.equal(sql('select count(*) from direct_pay_orders')[0][0],3)
 console.log('PASS Chromium actual login-cookie refresh, wallet restore/reload, credited hides checkout; no extra order')
 await page.getByRole('button',{name:'New recharge',exact:true}).click()
 await page.getByRole('button',{name:/Alipay PagePay/}).click()
 let aborted=false,keys=[]
 await page.route('**/api/user/direct-pay/orders',async route=>{
  keys.push(route.request().headers()['idempotency-key'])
  if(!aborted){aborted=true;await route.fetch();await route.abort('failed')}else await route.continue()
 })
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.waitForFunction(()=>JSON.parse(localStorage.getItem('direct-payment-orders-v1')).state.purchases['1'])
 // Wait for the completed server-side create before reloading away its lost response.
 for(let i=0;i<40&&sql('select count(*) from direct_pay_orders')[0][0]!==4;i++)await delay(100)
 assert.equal(sql('select count(*) from direct_pay_orders')[0][0],4)
 await page.reload();await page.getByRole('button',{name:'Resume payment request',exact:true}).click()
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Go to Alipay',exact:true}).waitFor()
 assert.equal(keys.length,2);assert.equal(keys[0],keys[1]);assert.equal(sql('select count(*) from direct_pay_orders')[0][0],4)
 console.log('PASS Chromium committed-create response abort, reload/resume uses same key and same order')
 // Simulated popup blocker: browser remains on pending order and can retry.
 await page.evaluate(()=>{window.open=()=>null})
 await page.getByRole('button',{name:'Go to Alipay',exact:true}).click()
 assert.match(await page.locator('body').innerText(),/pending/)
 assert.equal(sql('select count(*) from direct_pay_ledgers')[0][0],3)
 console.log('PASS Chromium blocked popup does not report credit or lose checkout')
 const pending=sql("select order_no from direct_pay_orders where settlement_state='uncredited'")[0][0]
 sql('update direct_pay_orders set expires_at=1 where order_no=?',[pending])
 await page.getByText('Checkout expired. Payment verification continues.',{exact:true}).waitFor({timeout:15000})
 assert.equal(await page.getByRole('button',{name:'Go to Alipay',exact:true}).count(),0)
 console.log('PASS Chromium expired checkout hidden while order polling continues')
 await page.screenshot({path:resolve(root,'docs/direct-pay/evidence/browser-wallet.png'),fullPage:true})
 await context.clearCookies();await page.reload();await page.waitForURL('**/sign-in**',{timeout:15000})
 console.log('PASS Chromium login loss redirects to sign-in without creating order')
 if(containerMode){
  const row=sql('select order_no,money_minor,quota from direct_pay_orders where order_no=?',[pending])[0]
  const pendingOrder={order_no:row[0],money_minor:row[1],quota_to_credit:row[2]}
  function redis(...args){const r=spawnSync('docker',['exec','newapi-directpay-test-redis','redis-cli','--raw','-n','7',...args],{encoding:'utf8'});assert.equal(r.status,0);return r.stdout.trim()}
  const cached=Number(redis('HGET','user:1','Quota'));assert.equal(cached,before+3*snapshot.quota)
  redis('HINCRBY','user:1','Quota','-700')
  assert.equal(spawnSync('docker',['stop','-t','5','newapi-directpay-test-redis'],{stdio:'ignore'}).status,0)
  try{
   const failed=await fetch(origin+'/api/direct-pay/notify/fixture.v1',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:notification(pendingOrder)})
   assert.ok(failed.status>=500,'Redis-backed rate limiter must not falsely ACK')
   assert.equal(sql('select count(*) from direct_pay_ledgers')[0][0],3)
  }finally{assert.equal(spawnSync('docker',['start','newapi-directpay-test-redis'],{stdio:'ignore'}).status,0)}
  for(let i=0;i<40;i++){if(redis('PING')==='PONG')break;await delay(100)}
  let delivered=false
  for(let i=0;i<40;i++){try{await notify(pendingOrder);delivered=true;break}catch{await delay(250)}}
  assert.ok(delivered,'callback retry must recover after Redis DNS/reconnect')
  assert.equal(sql('select settlement_state from direct_pay_orders where order_no=?',[pending])[0][0],'uncredited')
  assert.equal(spawnSync('docker',['stop','-t','5','newapi-directpay-test-redis'],{stdio:'ignore'}).status,0)
  try{await credited(pendingOrder)}finally{assert.equal(spawnSync('docker',['start','newapi-directpay-test-redis'],{stdio:'ignore'}).status,0)}
  let recovered=0
  for(let i=0;i<60;i++){try{recovered=Number(redis('HGET','user:1','Quota'));if(recovered===cached+pendingOrder.quota_to_credit-700)break}catch{}await delay(250)}
  assert.equal(recovered,cached+pendingOrder.quota_to_credit-700)
  assert.equal(sql('select count(*) from direct_pay_ledgers')[0][0],4)
  console.log('PASS Redis outage returns retryable non-ACK; after durable ACK, Redis outage does not prevent DB credit; restart retains 700 reservation and replay adds once')
 }
 console.log('REHEARSAL_COMPLETE fixture-only; no official payment transport, no real funds')
}finally{if(browser)await browser.close();await Promise.all(children.map(stop))}
