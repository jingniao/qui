<template>
  <div class="min-h-screen bg-[#F2F2F7] flex items-center justify-center p-4">
    <div class="max-w-md w-full bg-white/95 backdrop-blur-2xl rounded-3xl shadow-ios-lg border border-black/[0.08] p-8 text-center space-y-4">
      <div class="w-16 h-16 rounded-2xl bg-[#FF9500]/10 text-[#FF9500] flex items-center justify-center mx-auto">
        <svg class="w-8 h-8" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
        </svg>
      </div>
      <h1 class="text-4xl font-extrabold text-[#1C1C1E] tracking-tight font-mono">403</h1>
      <h2 class="text-base font-bold text-[#1C1C1E]">访问权限受限</h2>
      <p class="text-xs text-[#8E8E93] leading-relaxed">
        您当前的身份角色没有权限访问此模块或执行该敏感操作。请联系系统所有者 (Owner) 获取更高管理权限。
      </p>
      <div class="flex items-center justify-center gap-3 pt-2">
        <button
          @click="$router.back()"
          class="px-5 py-2 text-xs font-semibold rounded-full bg-black/[0.05] hover:bg-black/[0.08] text-[#1C1C1E] transition-all"
        >
          返回上一页
        </button>
        <router-link
          to="/dashboard"
          class="px-5 py-2 text-xs font-semibold rounded-full text-white bg-[#007AFF] hover:bg-[#0071E3] transition-all shadow-[0_2px_8px_rgba(0,122,255,0.25)]"
        >
          返回系统总览
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// 403 Forbidden Page
</script>
