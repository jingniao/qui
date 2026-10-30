import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';
import MainLayout from '../layouts/MainLayout.vue';
import { setupNavigationGuards } from './guards';

const routes: RouteRecordRaw[] = [
  { path: '/portal/login', component: () => import('../portal/LoginPage.vue'), meta: { title: '用户登录' } },
  { path: '/portal', redirect: '/portal/overview' },
  { path: '/portal/client', redirect: '/portal/overview' },
  { path: '/portal/:section(overview|subscription|nodes|traffic|account)', component: () => import('../portal/PortalLayout.vue'), meta: { title: '我的账号' } },
  { path: '/portal-preview', component: () => import('../modules/users/pages/PortalPreviewPage.vue'), meta: { title: '用户门户预览' } },
  // Public auth routes
  {
    path: '/login',
    name: 'Login',
    component: () => import('../modules/auth/pages/LoginPage.vue'),
    meta: { title: '登录' }
  },
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('../modules/auth/pages/InitialSetupPage.vue'),
    meta: { title: '系统首次初始化' }
  },
  {
    path: '/403',
    name: 'Forbidden',
    component: () => import('../modules/auth/pages/ForbiddenPage.vue'),
    meta: { title: '无权访问' }
  },
  {
    path: '/404',
    name: 'NotFound',
    component: () => import('../modules/auth/pages/NotFoundPage.vue'),
    meta: { title: '页面不存在' }
  },

  // Main application routes wrapped with MainLayout
  {
    path: '/',
    component: MainLayout,
    redirect: '/dashboard',
    children: [
      // 1. Dashboard
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('../modules/dashboard/pages/DashboardPage.vue'),
        meta: { title: '系统总览', icon: 'LayoutDashboard' }
      },

      // 2. User Module (用户管理)
      {
        path: 'users',
        name: 'UserList',
        component: () => import('../modules/users/pages/UserListPage.vue'),
        meta: { title: '用户', parent: '用户管理' }
      },
      {
        path: 'users/:id',
        name: 'UserDetail',
        component: () => import('../modules/users/pages/UserDetailPage.vue'),
        meta: { title: '用户详情', parent: '用户管理' }
      },
      {
        path: 'devices',
        redirect: '/users'
      },
      {
        path: 'subscriptions',
        redirect: '/users'
      },

      { path: 'system/admins', redirect: '/users/admins' },
      { path: 'nodes/versions', component: () => import('../modules/nodes/pages/NodeVersionsPage.vue'), meta: { title: '版本与更新', parent: '服务器' } },
      { path: 'proxy/groups', component: () => import('../modules/proxy/pages/LineGroupPage.vue'), meta: { title: '线路组', parent: '节点配置' } },
      // 3. Node Module
      {
        path: 'nodes',
        name: 'NodeList',
        component: () => import('../modules/nodes/pages/NodeListPage.vue'),
        meta: { title: '服务器列表', description: '以“列表看状态、详情做管理”为原则，保持简洁清晰。', parent: '服务器' }
      },
      {
        path: 'nodes/:id',
        name: 'NodeDetail',
        component: () => import('../modules/nodes/pages/NodeDetailPage.vue'),
        meta: { title: '服务器详情', parent: '服务器' }
      },
      { path: 'node-groups', redirect: '/proxy/groups' },
      { path: 'topology', redirect: '/proxy/relay' },

      // 4. Proxy Module (Unified Node List & Relay Canvas)
      {
        path: 'proxy',
        redirect: '/proxy/nodes'
      },
      {
        path: 'proxy/nodes',
        name: 'ProxyNodeList',
        component: () => import('../modules/proxy/pages/ProxyNodeListPage.vue'),
        meta: { title: '节点列表', parent: '节点配置' }
      },
      {
        path: 'proxy/relay',
        name: 'RelayCanvas',
        component: () => import('../modules/proxy/pages/RelayCanvasPage.vue'),
        meta: { title: '中转搭建', parent: '节点配置' }
      },
      // Backward Compatibility Redirects
      {
        path: 'proxy/inbounds',
        redirect: '/proxy/nodes'
      },
      {
        path: 'proxy/outbounds',
        redirect: '/proxy/nodes'
      },
      {
        path: 'proxy/outbound-groups',
        redirect: '/proxy/nodes'
      },
      {
        path: 'proxy/chains',
        redirect: '/proxy/relay'
      },
      {
        path: 'proxy/chains/new',
        redirect: '/proxy/relay'
      },
      {
        path: 'proxy/chains/:id/edit',
        redirect: '/proxy/relay'
      },

      // 5. Routing Module (Client Policies, Node Policies, Rule Sets, Simulator)
      {
        path: 'routing',
        redirect: '/routing/client'
      },
      {
        path: 'routing/client',
        name: 'ClientPolicy',
        component: () => import('../modules/routing/pages/ClientPolicyPage.vue'),
        meta: { title: '客户端策略', parent: '分流控制' }
      },
      {
        path: 'routing/rules',
        name: 'RoutingRuleList',
        component: () => import('../modules/routing/pages/RoutingRulePage.vue'),
        meta: { title: '节点策略', parent: '分流控制' }
      },
      {
        path: 'routing/rules/new',
        name: 'RoutingRuleCreate',
        component: () => import('../modules/routing/pages/RoutingRuleEditorPage.vue'),
        meta: { title: '新建节点策略', parent: '分流控制' }
      },
      {
        path: 'routing/rules/:id/edit',
        name: 'RoutingRuleEdit',
        component: () => import('../modules/routing/pages/RoutingRuleEditorPage.vue'),
        meta: { title: '编辑节点策略', parent: '分流控制' }
      },
      {
        path: 'routing/services',
        name: 'RuleSetList',
        component: () => import('../modules/routing/pages/ServiceRulePage.vue'),
        meta: { title: '规则集', parent: '分流控制' }
      },
      {
        path: 'routing/rule-sets',
        redirect: '/routing/services'
      },
      {
        path: 'routing/websites',
        redirect: '/routing/rules'
      },
      {
        path: 'routing/simulator',
        name: 'PolicySimulator',
        component: () => import('../modules/routing/pages/PolicySimulatorPage.vue'),
        meta: { title: '策略模拟', parent: '分流控制' }
      },

      // 6. Access Records Module
      {
        path: 'access',
        name: 'AccessRecord',
        component: () => import('../modules/access/pages/AccessRecordPage.vue'),
        meta: { title: '访问记录' }
      },

      // 7. System Module
      {
        path: 'users/admins',
        name: 'AdminList',
        component: () => import('../modules/system/pages/AdminPage.vue'),
        meta: { title: '管理员', parent: '用户管理' }
      },
      {
        path: 'system/agents',
        redirect: '/nodes/versions?tab=agents'
      },
      {
        path: 'system/cores',
        redirect: '/nodes/versions?tab=cores'
      },
      {
        path: 'system/revisions',
        redirect: '/nodes'
      },
      {
        path: 'system/backups',
        name: 'BackupList',
        component: () => import('../modules/system/pages/BackupPage.vue'),
        meta: { title: '备份恢复', parent: '系统' }
      },
      {
        path: 'system/operations',
        name: 'OperationLogList',
        component: () => import('../modules/system/pages/OperationLogPage.vue'),
        meta: { title: '操作日志', parent: '系统' }
      },
      {
        path: 'system/settings',
        name: 'SystemSettings',
        component: () => import('../modules/system/pages/SettingsPage.vue'),
        meta: { title: '系统设置', parent: '系统' }
      }
    ]
  },

  // Fallback wildcard
  {
    path: '/:pathMatch(.*)*',
    redirect: '/404'
  }
];

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior() {
    return { top: 0 };
  }
});

router.beforeEach((to) => {
  if (to.path === '/nodes' && (to.query.tab === 'agents' || to.query.tab === 'cores')) {
    return { path: '/nodes/versions', query: { ...to.query }, hash: to.hash };
  }
});
router.beforeEach(setupNavigationGuards);

router.afterEach((to) => {
  const baseTitle = 'Proxy Control';
  document.title = to.meta?.title ? `${to.meta.title} - ${baseTitle}` : baseTitle;
});
