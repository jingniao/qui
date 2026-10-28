<template>
 <section class="panel-card p-6 max-w-3xl space-y-5 text-sm">
  <div class="flex justify-between items-center gap-3"><h2 class="font-semibold">{{ title }}</h2><button class="ios-btn-secondary" :disabled="loading || saving" @click="load">刷新</button></div>
  <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="!info || !saved" />
  <template v-else>
   <template v-if="kind==='network'">
    <p class="text-gray-500">以下为当前服务实例的启动配置。修改监听地址和 TLS 请编辑服务器配置文件，并在维护窗口重启服务。</p>
    <dl class="grid sm:grid-cols-2 gap-4"><div><dt class="text-gray-500">Web / API 监听</dt><dd class="mt-1 font-mono">{{ info.network.httpAddress }}</dd></div><div><dt class="text-gray-500">节点控制监听</dt><dd class="mt-1 font-mono">{{ info.network.grpcAddress }}</dd></div><div><dt class="text-gray-500">运行模式</dt><dd class="mt-1">{{ info.network.development ? '本地开发' : '生产' }}</dd></div><div><dt class="text-gray-500">TLS</dt><dd class="mt-1">{{ info.network.tlsEnabled ? `已启用，最低 ${info.network.minimumTLS}` : '开发模式未启用 Web TLS' }}</dd></div><div><dt class="text-gray-500">配置证书文件有效期</dt><dd class="mt-1">{{ info.network.certificate.available ? `${formatTime(info.network.certificate.notBefore || '')} 至 ${formatTime(info.network.certificate.notAfter || '')}` : '尚未取得证书信息' }}</dd></div><div><dt class="text-gray-500">证书文件当前有效</dt><dd class="mt-1">{{ info.network.certificate.available ? (info.network.certificate.validNow ? '是' : '否') : '未知' }}</dd></div></dl>
    <p class="text-xs text-gray-500">证书信息读取配置指定的本地文件。替换文件后需要重启使新证书生效；此处不展示私钥或证书正文。</p>
   </template>
   <template v-else-if="kind==='security'">
    <label class="block">新登录会话有效时间（分钟）<input v-model.number="form.sessionMinutes" type="number" min="15" max="1440" step="1" class="form-input-base mt-2" /></label>
    <p class="text-xs text-gray-500">可设置 15～1440 分钟，保存后对之后的新登录生效。已有会话保持原到期时间，修改密码会撤销该管理员的已有会话。</p>
    <dl class="grid sm:grid-cols-2 gap-4"><div><dt class="text-gray-500">登录限制</dt><dd class="mt-1">每个来源 {{ info.security.loginWindowMinutes }} 分钟内 {{ info.security.loginAttempts }} 次尝试</dd></div><div><dt class="text-gray-500">密码长度</dt><dd class="mt-1">{{ info.security.passwordMinCharacters }}～{{ info.security.passwordMaxCharacters }} 字符</dd></div><div><dt class="text-gray-500">Cookie</dt><dd class="mt-1">HttpOnly · {{ info.security.sameSite }}{{ info.security.secureCookies ? ' · Secure' : ' · 开发模式' }}</dd></div><div><dt class="text-gray-500">请求保护</dt><dd class="mt-1">同源 / CSRF；普通 JSON 最大 {{ bytes(info.security.requestBytes) }}</dd></div></dl>
   </template>
   <template v-else-if="kind==='subscription'">
    <label class="block">订阅链接默认格式<select v-model="form.subscriptionFormat" class="form-input-base mt-2"><option v-for="item in formats" :key="item.id" :value="item.id">{{ formatNames[item.id] || item.id }}</option></select></label>
    <p class="text-gray-500">未指定 format 的订阅请求使用这个格式。带 ?format= 的链接保持显式选择（{{ formats.map(item => item.id).join('、') }}）；客户端需支持对应格式。Clash 格式带有「客户端策略」中的分流规则。</p>
    <div class="grid sm:grid-cols-2 gap-4">
     <label class="block">客户端刷新间隔（小时）<input v-model.number="form.subscriptionUpdateHours" type="number" min="1" max="168" step="1" class="form-input-base mt-2" /></label>
     <label class="flex items-center gap-2 sm:mt-7"><input v-model="form.subscriptionUaDetection" type="checkbox" class="h-4 w-4" />按客户端 User-Agent 自动选择格式</label>
    </div>
    <p class="text-xs text-gray-500">刷新间隔写入订阅响应的 profile-update-interval，客户端据此定期重新拉取（1～168 小时）。目前没有可验证的客户端 User-Agent 特征，自动识别即使开启也只会回落到默认格式；关闭后始终使用默认格式。</p>
    <p class="text-xs text-gray-500">订阅继续按用户授权节点组、已发布配置、有效设备凭据和协议能力过滤。默认格式不会改变用户权限或正在运行的节点配置。</p>
   </template>
   <template v-else>
    <dl class="grid sm:grid-cols-2 gap-4"><div><dt class="text-gray-500">构建标识</dt><dd class="mt-1">{{ info.buildVersion }}</dd></div><div><dt class="text-gray-500">系统 / 架构</dt><dd class="mt-1">{{ info.os }} / {{ info.arch }}</dd></div><div><dt class="text-gray-500">Go</dt><dd class="mt-1">{{ info.goVersion }}</dd></div><div><dt class="text-gray-500">当前实例启动时间</dt><dd class="mt-1">{{ formatTime(info.startedAt) }} · 已运行 {{ Math.floor(info.uptimeSeconds/60) }} 分钟</dd></div><div><dt class="text-gray-500">SQLite / 日志模式</dt><dd class="mt-1">{{ info.sqliteVersion }} / {{ info.journalMode }}</dd></div><div><dt class="text-gray-500">最新数据库迁移</dt><dd class="mt-1 break-all">{{ info.migration || '暂无' }}</dd></div><div><dt class="text-gray-500">数据库主文件</dt><dd class="mt-1">{{ bytes(info.databaseBytes) }}，不含 WAL</dd></div><div><dt class="text-gray-500">Go 堆 / Goroutine</dt><dd class="mt-1">{{ bytes(info.heapBytes) }} / {{ info.goroutines }}</dd></div></dl>
    <p class="text-xs text-gray-500">采样时间 {{ formatTime(info.generatedAt) }}。构建标识来自程序内的 Go 构建信息，开发构建可能显示 (devel)。恢复时重建服务实例会重新计算运行时长。</p>
   </template>
   <div v-if="kind==='security' || kind==='subscription'" class="flex flex-wrap justify-between items-center gap-3"><span class="text-xs text-gray-500">{{ saved.revision ? `版本 ${saved.revision} · ${saved.updatedBy} · ${formatTime(saved.updatedAt)}` : '当前为默认设置' }}</span><button class="ios-btn-primary" :disabled="loading || saving" @click="save">{{ saving ? '保存中…' : '保存设置' }}</button></div>
  </template>
 </section>
</template>
<script setup lang="ts">
import { computed,onMounted,reactive,ref } from 'vue';
import { settingsAPI,type RuntimeInfo,type RuntimeSettings,type SubscriptionFormatInfo } from '../settingsApi';
import { formatTime } from '../../../app/auth';import { showToast } from '../../../app/api';
import ErrorState from '../../../components/ErrorState.vue';import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const props=defineProps<{kind:'network'|'security'|'subscription'|'info'}>();
const title=computed(()=>({network:'当前网络配置',security:'认证与会话安全',subscription:'订阅默认设置',info:'系统运行信息'}[props.kind]));
const info=ref<RuntimeInfo|null>(null),saved=ref<RuntimeSettings|null>(null),loading=ref(false),saving=ref(false),error=ref('');
// The format list comes from the server registry, so a new format needs no change here.
const formats=ref<SubscriptionFormatInfo[]>([{id:'uri',aliases:[],contentType:'',fileName:'',usesRules:false}]);
const formatNames:Record<string,string>={uri:'通用 URI 列表',base64:'Base64 URI 列表','sing-box':'sing-box JSON（出站片段）',clash:'Clash / mihomo 完整配置','clash-legacy':'旧版 Clash / Clash Premium'};
const form=reactive({sessionMinutes:120,subscriptionFormat:'uri',subscriptionUpdateHours:12,subscriptionUaDetection:true});
function bytes(value:number){return value<1024*1024 ? `${(value/1024).toFixed(1)} KB` : `${(value/1024/1024).toFixed(1)} MB`;}
async function load(){if(loading.value||saving.value)return;loading.value=true;try{const [runtime,settings,registry]=await Promise.all([settingsAPI.runtime(),settingsAPI.runtimePolicy(),settingsAPI.subscriptionFormats()]);info.value=runtime;formats.value=registry.items;saved.value=settings;Object.assign(form,settings.policy);error.value='';}catch(e){error.value=(e as Error).message;}finally{loading.value=false;}}
async function save(){if(saving.value||!saved.value)return;if(!Number.isInteger(form.sessionMinutes)||form.sessionMinutes<15||form.sessionMinutes>1440){showToast('会话时间须为 15～1440 的整数','error');return;}if(props.kind==='subscription'&&(!Number.isInteger(form.subscriptionUpdateHours)||form.subscriptionUpdateHours<1||form.subscriptionUpdateHours>168)){showToast('刷新间隔须为 1～168 的整数','error');return;}saving.value=true;try{const policy={...saved.value.policy,...(props.kind==='security'?{sessionMinutes:form.sessionMinutes}:{subscriptionFormat:form.subscriptionFormat,subscriptionUpdateHours:form.subscriptionUpdateHours,subscriptionUaDetection:form.subscriptionUaDetection})};const result=await settingsAPI.saveRuntimePolicy(policy,saved.value.revision);saved.value=result;Object.assign(form,result.policy);error.value='';showToast('设置已保存');}catch(e){error.value=(e as Error).message;}finally{saving.value=false;}}
onMounted(load);
</script>
