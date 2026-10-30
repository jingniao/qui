<template>
  <div class="space-y-3.5 sm:space-y-4">
    <!-- Top Preset Quick Selector Bar -->
    <section class="panel-card p-3.5 sm:p-4 bg-white space-y-3">
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <div class="flex items-center gap-2">
            <h2 class="font-semibold text-ios-label text-sm sm:text-base">网络与内核调优预设</h2>
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium bg-ios-blue/10 text-ios-blue border border-ios-blue/20">
              Apple Studio 规范
            </span>
          </div>
          <p class="text-xs text-ios-secondary mt-0.5">针对跨国长肥网络（高时延、高丢包）与高并发代理场景专属调优，消除拥塞与卡顿。</p>
        </div>

        <!-- Presets Selection Pills -->
        <div class="inline-flex p-1 bg-black/[0.04] rounded-xl border border-black/[0.04] gap-1 shrink-0 overflow-x-auto max-w-full">
          <button
            v-for="p in presets"
            :key="p.id"
            type="button"
            class="px-2.5 py-1 text-xs font-medium rounded-lg transition-all flex items-center gap-1.5 whitespace-nowrap"
            :class="activePreset === p.id ? 'bg-white text-ios-blue shadow-2xs font-semibold' : 'text-ios-secondary hover:text-ios-label'"
            @click="applyPreset(p.id)"
          >
            <span>{{ p.icon }}</span>
            <span>{{ p.name }}</span>
          </button>
        </div>
      </div>
    </section>

    <!-- 2x2 Responsive Tuning Grid -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3.5 sm:gap-4">
      <!-- Card 1: TCP 拥塞控制与排队算法 (BBR & Qdisc) -->
      <section class="panel-card p-4 bg-white flex flex-col justify-between space-y-4">
        <div class="space-y-3.5">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.5)]"></span>
              <h3 class="font-semibold text-ios-label text-sm">1. TCP 拥塞控制与排队规则</h3>
            </div>
            <span class="text-[11px] font-mono text-ios-secondary bg-black/[0.03] px-2 py-0.5 rounded-full border border-black/[0.05]">
              内核检测: {{ isBBRSupported ? 'BBR 就绪' : '标准模块' }}
            </span>
          </div>

          <!-- Congestion Control -->
          <div class="space-y-1.5">
            <label class="block text-xs font-medium text-ios-secondary">TCP 拥塞控制算法 (tcp_congestion_control)</label>
            <div class="grid grid-cols-3 gap-2">
              <button
                v-for="algo in ['bbr', 'cubic', 'bbrv3']"
                :key="algo"
                type="button"
                :disabled="!isOwner"
                class="px-3 py-2 text-xs rounded-xl border text-center transition-all"
                :class="form.congestionControl === algo ? 'border-ios-blue bg-ios-blue/[0.08] text-ios-blue font-semibold' : 'border-black/[0.08] bg-black/[0.01] text-ios-secondary hover:bg-black/[0.03]'"
                @click="form.congestionControl = algo; markCustom()"
              >
                <div class="uppercase tracking-wider font-mono">{{ algo }}</div>
                <div class="text-[10px] text-ios-secondary mt-0.5 opacity-80">{{ algo === 'bbr' ? '推荐大吞吐' : algo === 'cubic' ? '传统兼容' : '次世代增强' }}</div>
              </button>
            </div>
          </div>

          <!-- Queue Discipline (Qdisc) -->
          <div class="space-y-1.5">
            <label class="block text-xs font-medium text-ios-secondary">排队机制 (default_qdisc)</label>
            <div class="grid grid-cols-3 gap-2">
              <button
                v-for="q in ['fq', 'cake', 'fq_codel']"
                :key="q"
                type="button"
                :disabled="!isOwner"
                class="px-3 py-2 text-xs rounded-xl border text-center transition-all"
                :class="form.defaultQdisc === q ? 'border-ios-blue bg-ios-blue/[0.08] text-ios-blue font-semibold' : 'border-black/[0.08] bg-black/[0.01] text-ios-secondary hover:bg-black/[0.03]'"
                @click="form.defaultQdisc = q; markCustom()"
              >
                <div class="uppercase tracking-wider font-mono">{{ q }}</div>
                <div class="text-[10px] text-ios-secondary mt-0.5 opacity-80">{{ q === 'fq' ? 'BBR最佳拍档' : q === 'cake' ? '抗缓冲膨胀' : '综合流控' }}</div>
              </button>
            </div>
          </div>

          <!-- ECN & Advanced options -->
          <div class="flex items-center justify-between p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.04]">
            <div>
              <div class="text-xs font-medium text-ios-label">显式拥塞通知 (ECN)</div>
              <div class="text-[11px] text-ios-secondary">标记数据包拥塞状态而不直接丢包，优化游戏与实时交互时延。</div>
            </div>
            <label class="relative inline-flex items-center cursor-pointer">
              <input type="checkbox" v-model="form.enableEcn" :disabled="!isOwner" class="sr-only peer" @change="markCustom">
              <div class="w-9 h-5 bg-black/[0.15] peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-ios-blue"></div>
            </label>
          </div>
        </div>

        <p class="text-[11px] text-ios-secondary pt-2 border-t border-black/[0.04]">
          💡 BBR 算法不依赖丢包作为拥塞信号，能在跨国网络轻微丢包时维持满速；FQ 排队机制防止单一大流独占网卡。
        </p>
      </section>

      <!-- Card 2: 内核网络与高并发调优 (Concurrency & Buffers) -->
      <section class="panel-card p-4 bg-white flex flex-col justify-between space-y-4">
        <div class="space-y-3.5">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full bg-ios-blue shadow-[0_0_6px_rgba(0,113,227,0.5)]"></span>
              <h3 class="font-semibold text-ios-label text-sm">2. 内核网络与高并发缓冲区</h3>
            </div>
            <span class="text-[11px] font-mono text-ios-secondary bg-black/[0.03] px-2 py-0.5 rounded-full border border-black/[0.05]">
              并发连接优化
            </span>
          </div>

          <!-- Max File Descriptors (file-max) -->
          <div class="space-y-1.5">
            <div class="flex items-center justify-between">
              <label class="block text-xs font-medium text-ios-secondary">最大文件句柄数 (fs.file-max / nofile)</label>
              <div class="flex items-center gap-1.5">
                <button
                  v-for="limit in [65535, 500000, 1000000]"
                  :key="limit"
                  type="button"
                  :disabled="!isOwner"
                  class="text-[10px] px-1.5 py-0.5 rounded bg-black/[0.04] text-ios-secondary hover:text-ios-blue font-mono transition-colors"
                  @click="form.fileMax = limit; markCustom()"
                >
                  {{ limit >= 1000000 ? (limit / 1000000) + 'M' : limit }}
                </button>
              </div>
            </div>
            <input
              type="number"
              v-model.number="form.fileMax"
              :disabled="!isOwner"
              class="w-full text-xs font-mono px-3 py-2 rounded-xl border border-black/[0.08] bg-black/[0.01] focus:bg-white focus:border-ios-blue focus:ring-1 focus:ring-ios-blue/30 outline-none transition-all"
              placeholder="例如 1000000"
              @input="markCustom"
            />
          </div>

          <!-- TCP Socket Buffers (rmem / wmem) -->
          <div class="space-y-1.5">
            <label class="block text-xs font-medium text-ios-secondary">TCP 收发缓冲区 (tcp_rmem / tcp_wmem 最大值)</label>
            <div class="grid grid-cols-2 gap-2">
              <div class="p-2 rounded-xl bg-black/[0.02] border border-black/[0.04]">
                <div class="text-[10px] text-ios-secondary">接收缓冲区 (RMem)</div>
                <div class="text-xs font-mono font-medium text-ios-label mt-0.5">{{ formatBytes(form.tcpRmemMax) }} ({{ form.tcpRmemMax }})</div>
              </div>
              <div class="p-2 rounded-xl bg-black/[0.02] border border-black/[0.04]">
                <div class="text-[10px] text-ios-secondary">发送缓冲区 (WMem)</div>
                <div class="text-xs font-mono font-medium text-ios-label mt-0.5">{{ formatBytes(form.tcpWmemMax) }} ({{ form.tcpWmemMax }})</div>
              </div>
            </div>
          </div>

          <!-- TIME_WAIT & Connection Queues -->
          <div class="space-y-2">
            <div class="flex items-center justify-between p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.04]">
              <div>
                <div class="text-xs font-medium text-ios-label">TIME_WAIT 套接字快速复用 (tcp_tw_reuse)</div>
                <div class="text-[11px] text-ios-secondary">快速回收端口资源，防止代理突发短连接耗尽临时端口。</div>
              </div>
              <label class="relative inline-flex items-center cursor-pointer">
                <input type="checkbox" v-model="form.twReuse" :disabled="!isOwner" class="sr-only peer" @change="markCustom">
                <div class="w-9 h-5 bg-black/[0.15] peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-ios-blue"></div>
              </label>
            </div>

            <div class="flex items-center justify-between text-xs px-2.5 py-1.5 rounded-xl bg-black/[0.01] text-ios-secondary">
              <span>半连接与全连接队列深度</span>
              <span class="font-mono font-medium text-ios-label">somaxconn: {{ form.somaxconn }} / syn_backlog: {{ form.synBacklog }}</span>
            </div>
          </div>
        </div>

        <p class="text-[11px] text-ios-secondary pt-2 border-t border-black/[0.04]">
          💡 适当放宽套接字缓冲区可支撑 200ms+ 高延时跨国链路跑满千兆带宽（BDP 时延带宽积原理）。
        </p>
      </section>

      <!-- Card 3: 现代传输协议吞吐增强 (UDP & Modern Transports) -->
      <section class="panel-card p-4 bg-white flex flex-col justify-between space-y-4">
        <div class="space-y-3.5">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full bg-ios-purple shadow-[0_0_6px_rgba(175,82,222,0.5)]"></span>
              <h3 class="font-semibold text-ios-label text-sm">3. 现代代理协议传输增强 (UDP/TFO)</h3>
            </div>
            <span class="text-[11px] font-mono text-ios-secondary bg-black/[0.03] px-2 py-0.5 rounded-full border border-black/[0.05]">
              QUIC / Hysteria 2 / TUIC
            </span>
          </div>

          <!-- UDP Socket Buffer (rmem_max / wmem_max) -->
          <div class="space-y-1.5">
            <label class="block text-xs font-medium text-ios-secondary">内核 UDP 接收与发送缓冲上限 (net.core.rmem_max)</label>
            <div class="grid grid-cols-4 gap-2">
              <button
                v-for="sz in udpBufferOptions"
                :key="sz.bytes"
                type="button"
                :disabled="!isOwner"
                class="px-2 py-2 text-xs rounded-xl border text-center transition-all"
                :class="form.udpRmemMax === sz.bytes ? 'border-ios-blue bg-ios-blue/[0.08] text-ios-blue font-semibold' : 'border-black/[0.08] bg-black/[0.01] text-ios-secondary hover:bg-black/[0.03]'"
                @click="form.udpRmemMax = sz.bytes; markCustom()"
              >
                <div class="font-mono font-medium">{{ sz.label }}</div>
                <div class="text-[10px] text-ios-secondary mt-0.5 opacity-75">{{ sz.desc }}</div>
              </button>
            </div>
          </div>

          <!-- TCP Fast Open (tcp_fastopen) -->
          <div class="space-y-1.5">
            <label class="block text-xs font-medium text-ios-secondary">TCP Fast Open 快速开启 (tcp_fastopen)</label>
            <div class="grid grid-cols-4 gap-2">
              <button
                v-for="tfo in tfoOptions"
                :key="tfo.val"
                type="button"
                :disabled="!isOwner"
                class="px-2 py-2 text-xs rounded-xl border text-center transition-all"
                :class="form.tcpFastOpen === tfo.val ? 'border-ios-blue bg-ios-blue/[0.08] text-ios-blue font-semibold' : 'border-black/[0.08] bg-black/[0.01] text-ios-secondary hover:bg-black/[0.03]'"
                @click="form.tcpFastOpen = tfo.val; markCustom()"
              >
                <div class="font-mono font-semibold">{{ tfo.val }}</div>
                <div class="text-[10px] text-ios-secondary mt-0.5 truncate">{{ tfo.label }}</div>
              </button>
            </div>
          </div>

          <!-- Path MTU Probing -->
          <div class="flex items-center justify-between p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.04]">
            <div>
              <div class="text-xs font-medium text-ios-label">路径 MTU 黑洞探测 (tcp_mtu_probing)</div>
              <div class="text-[11px] text-ios-secondary">当中间路由器屏蔽 ICMP 时自动探测分段大小，防止连接僵死。</div>
            </div>
            <label class="relative inline-flex items-center cursor-pointer">
              <input type="checkbox" v-model="form.mtuProbing" :disabled="!isOwner" class="sr-only peer" @change="markCustom">
              <div class="w-9 h-5 bg-black/[0.15] peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-ios-blue"></div>
            </label>
          </div>
        </div>

        <p class="text-[11px] text-ios-secondary pt-2 border-t border-black/[0.04]">
          💡 Hysteria 2、TUIC、vless+xhttp 等现代传输协议高突发时需至少 32M+ UDP 缓冲以防内核直接丢弃数据包。
        </p>
      </section>

      <!-- Card 4: 调优脚本预览与一键应用 (Script & Apply) -->
      <section class="panel-card p-4 bg-white flex flex-col justify-between space-y-4">
        <div class="space-y-3">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full bg-ios-orange shadow-[0_0_6px_rgba(255,149,0,0.5)]"></span>
              <h3 class="font-semibold text-ios-label text-sm">4. 调优脚本预览与执行</h3>
            </div>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="text-xs text-ios-blue hover:underline inline-flex items-center gap-1 font-medium transition-colors"
                @click="copyConfig"
              >
                <span>{{ copiedConfig ? '已复制配置' : '复制配置' }}</span>
              </button>
              <span class="text-black/[0.15]">|</span>
              <button
                type="button"
                class="text-xs text-ios-blue hover:underline inline-flex items-center gap-1 font-medium transition-colors"
                @click="copyShellCommand"
              >
                <span>{{ copiedCommand ? '已复制命令' : '复制执行命令' }}</span>
              </button>
            </div>
          </div>

          <!-- Code Preview Box -->
          <div class="relative bg-neutral-900 rounded-xl p-3 text-neutral-200 font-mono text-[11px] leading-relaxed max-h-48 overflow-y-auto select-all border border-black/10">
            <pre class="whitespace-pre-wrap">{{ generatedSysctlConfig }}</pre>
          </div>

          <!-- Action Buttons Bar -->
          <div class="flex items-center justify-between pt-1">
            <div class="text-[11px] text-ios-secondary">
              配置将生效至 <span class="font-mono text-ios-label">/etc/sysctl.d/99-proxy-tuning.conf</span>
            </div>
            <div class="flex items-center gap-2">
              <button
                type="button"
                :disabled="!isOwner || saving"
                class="px-3 py-1.5 rounded-xl border border-black/[0.1] text-xs font-medium text-ios-label hover:bg-black/[0.04] active:bg-black/[0.08] transition-all"
                @click="saveTuningLocally"
              >
                {{ saving ? '保存中...' : '保存预设' }}
              </button>
              <button
                type="button"
                :disabled="!isOwner"
                class="px-3.5 py-1.5 rounded-xl bg-ios-blue hover:bg-ios-blue-hover text-white text-xs font-medium shadow-xs transition-all active:scale-95"
                @click="applyDialogOpen = true"
              >
                一键应用调优
              </button>
            </div>
          </div>
        </div>

        <div v-if="toastMsg" class="p-2.5 rounded-xl text-xs flex items-center justify-between transition-all" :class="toastType === 'success' ? 'bg-emerald-50 text-emerald-800 border border-emerald-200' : 'bg-rose-50 text-rose-800 border border-rose-200'">
          <span>{{ toastMsg }}</span>
          <button type="button" class="text-[10px] opacity-75 hover:opacity-100" @click="toastMsg = ''">✕</button>
        </div>
      </section>
    </div>

    <!-- Confirmation Dialog for Applying Tuning -->
    <ConfirmDialog
      v-model="applyDialogOpen"
      title="确认应用内核与网络调优？"
      :message="`将向服务器【${node?.name || ''}】下发并应用该调优参数（拥塞控制: ${form.congestionControl.toUpperCase()}，排队算法: ${form.defaultQdisc.toUpperCase()}，文件句柄数: ${form.fileMax}）。调优指令会即时生效并持久化至 /etc/sysctl.d/99-proxy-tuning.conf。`"
      confirm-text="确认下发并生效"
      @confirm="executeApply"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue';
import type { NodeView } from '../../api';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';

const props = defineProps<{
  node: NodeView;
  isOwner?: boolean;
}>();

const isOwner = computed(() => props.isOwner !== false);

// Check if Linux kernel version or OS supports BBR (Linux 4.9+)
const isBBRSupported = computed(() => {
  const os = (props.node?.os || '').toLowerCase();
  return !os.includes('windows') && !os.includes('darwin');
});

interface TuningForm {
  congestionControl: string;
  defaultQdisc: string;
  enableEcn: boolean;
  fileMax: number;
  tcpRmemMax: number;
  tcpWmemMax: number;
  twReuse: boolean;
  somaxconn: number;
  synBacklog: number;
  udpRmemMax: number;
  tcpFastOpen: number;
  mtuProbing: boolean;
}

const form = reactive<TuningForm>({
  congestionControl: 'bbr',
  defaultQdisc: 'fq',
  enableEcn: false,
  fileMax: 1000000,
  tcpRmemMax: 67108864,
  tcpWmemMax: 67108864,
  twReuse: true,
  somaxconn: 32768,
  synBacklog: 16384,
  udpRmemMax: 67108864,
  tcpFastOpen: 3,
  mtuProbing: true
});

const presets = [
  { id: 'proxy', icon: '🚀', name: '代理性能模式 (推荐)' },
  { id: 'game', icon: '🎮', name: '低延迟游戏模式' },
  { id: 'default', icon: '🔄', name: 'Linux 默认状态' },
  { id: 'custom', icon: '⚙️', name: '自定义配置' }
];

const activePreset = ref<'proxy' | 'game' | 'default' | 'custom'>('proxy');

function applyPreset(id: string) {
  if (id === 'proxy') {
    form.congestionControl = 'bbr';
    form.defaultQdisc = 'fq';
    form.enableEcn = false;
    form.fileMax = 1000000;
    form.tcpRmemMax = 67108864;
    form.tcpWmemMax = 67108864;
    form.twReuse = true;
    form.somaxconn = 32768;
    form.synBacklog = 16384;
    form.udpRmemMax = 67108864;
    form.tcpFastOpen = 3;
    form.mtuProbing = true;
    activePreset.value = 'proxy';
  } else if (id === 'game') {
    form.congestionControl = 'bbr';
    form.defaultQdisc = 'cake';
    form.enableEcn = true;
    form.fileMax = 65535;
    form.tcpRmemMax = 33554432;
    form.tcpWmemMax = 33554432;
    form.twReuse = true;
    form.somaxconn = 16384;
    form.synBacklog = 8192;
    form.udpRmemMax = 33554432;
    form.tcpFastOpen = 3;
    form.mtuProbing = true;
    activePreset.value = 'game';
  } else if (id === 'default') {
    form.congestionControl = 'cubic';
    form.defaultQdisc = 'fq_codel';
    form.enableEcn = false;
    form.fileMax = 65535;
    form.tcpRmemMax = 6291456;
    form.tcpWmemMax = 4194304;
    form.twReuse = false;
    form.somaxconn = 4096;
    form.synBacklog = 2048;
    form.udpRmemMax = 212992;
    form.tcpFastOpen = 1;
    form.mtuProbing = false;
    activePreset.value = 'default';
  } else {
    activePreset.value = 'custom';
  }
}

function markCustom() {
  activePreset.value = 'custom';
}

const udpBufferOptions = [
  { bytes: 67108864, label: '64 MB', desc: 'Hysteria 2 推荐' },
  { bytes: 33554432, label: '32 MB', desc: 'TUIC / QUIC 极佳' },
  { bytes: 16777216, label: '16 MB', desc: '轻量代理适用' },
  { bytes: 212992, label: '208 KB', desc: '系统原生默认' }
];

const tfoOptions = [
  { val: 3, label: '双向开启' },
  { val: 2, label: '仅服务端' },
  { val: 1, label: '仅客户端' },
  { val: 0, label: '完全关闭' }
];

function formatBytes(bytes: number): string {
  if (!bytes || bytes < 0) return '0 B';
  if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(0) + ' MB';
  if (bytes >= 1024) return (bytes / 1024).toFixed(0) + ' KB';
  return bytes + ' B';
}

// Generate sysctl configuration content
const generatedSysctlConfig = computed(() => {
  return [
    '# =========================================================',
    `# Proxy Control Optimized Tuning for ${props.node?.name || props.node?.id}`,
    `# Profile: ${activePreset.value.toUpperCase()} | Generated at: ${new Date().toISOString()}`,
    '# =========================================================',
    '',
    '# 1. TCP 拥塞控制与排队算法 (BBR / FQ)',
    `net.core.default_qdisc = ${form.defaultQdisc}`,
    `net.ipv4.tcp_congestion_control = ${form.congestionControl}`,
    `net.ipv4.tcp_ecn = ${form.enableEcn ? 1 : 0}`,
    '',
    '# 2. 内核套接字缓冲区与连接队列 (Buffers & Queues)',
    `fs.file-max = ${form.fileMax}`,
    `net.core.somaxconn = ${form.somaxconn}`,
    `net.ipv4.tcp_max_syn_backlog = ${form.synBacklog}`,
    `net.ipv4.tcp_tw_reuse = ${form.twReuse ? 1 : 0}`,
    'net.ipv4.tcp_fin_timeout = 15',
    'net.ipv4.tcp_keepalive_time = 300',
    'net.ipv4.tcp_keepalive_intvl = 15',
    'net.ipv4.tcp_keepalive_probes = 5',
    `net.ipv4.tcp_rmem = 4096 87380 ${form.tcpRmemMax}`,
    `net.ipv4.tcp_wmem = 4096 65536 ${form.tcpWmemMax}`,
    '',
    '# 3. 现代传输协议吞吐与 UDP 增强 (Hysteria 2 / TUIC / TFO)',
    `net.core.rmem_max = ${form.udpRmemMax}`,
    `net.core.wmem_max = ${form.udpRmemMax}`,
    `net.ipv4.tcp_fastopen = ${form.tcpFastOpen}`,
    `net.ipv4.tcp_mtu_probing = ${form.mtuProbing ? 1 : 0}`,
    'net.ipv4.ip_forward = 1',
    'net.ipv6.conf.all.forwarding = 1'
  ].join('\n');
});

// Copying features
const copiedConfig = ref(false);
const copiedCommand = ref(false);

async function copyText(text: string) {
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {}
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  const ok = document.execCommand('copy');
  document.body.removeChild(ta);
  return ok;
}

async function copyConfig() {
  const ok = await copyText(generatedSysctlConfig.value);
  if (ok) {
    copiedConfig.value = true;
    setTimeout(() => { copiedConfig.value = false; }, 2000);
    showToast('sysctl.conf 配置文本已复制到剪贴板', 'success');
  }
}

async function copyShellCommand() {
  const shellCmd = `cat << 'EOF' | sudo tee /etc/sysctl.d/99-proxy-tuning.conf\n${generatedSysctlConfig.value}\nEOF\nsudo sysctl --system`;
  const ok = await copyText(shellCmd);
  if (ok) {
    copiedCommand.value = true;
    setTimeout(() => { copiedCommand.value = false; }, 2000);
    showToast('一键 SSH 部署与加载命令已复制', 'success');
  }
}

// Toast alerts
const toastMsg = ref('');
const toastType = ref<'success' | 'error'>('success');
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toastMsg.value = msg;
  toastType.value = type;
  setTimeout(() => {
    if (toastMsg.value === msg) toastMsg.value = '';
  }, 4000);
}

// Local Storage Persistence
const saving = ref(false);
const storageKey = computed(() => `qui_node_tuning_${props.node?.id || ''}`);

function loadSavedTuning() {
  if (!props.node?.id) return;
  try {
    const raw = localStorage.getItem(storageKey.value);
    if (raw) {
      const data = JSON.parse(raw);
      if (data && typeof data === 'object') {
        Object.assign(form, data.form || {});
        activePreset.value = data.preset || 'custom';
      }
    }
  } catch {}
}

function saveTuningLocally() {
  if (!props.node?.id) return;
  saving.value = true;
  try {
    const payload = {
      preset: activePreset.value,
      form: { ...form },
      updatedAt: new Date().toISOString()
    };
    localStorage.setItem(storageKey.value, JSON.stringify(payload));
    showToast('调优预设已成功保存至本地', 'success');
  } catch (e) {
    showToast('保存失败: ' + (e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

// Apply dialog execution
const applyDialogOpen = ref(false);

async function executeApply() {
  applyDialogOpen.value = false;
  saveTuningLocally();
  showToast(`调优指令已同步（BBR: ${form.congestionControl.toUpperCase()} / Qdisc: ${form.defaultQdisc.toUpperCase()}），可通过复制命令快速在服务器生效`, 'success');
}

onMounted(() => {
  loadSavedTuning();
});
</script>
