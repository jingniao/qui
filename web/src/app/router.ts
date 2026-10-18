import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';
import MainLayout from '../layouts/MainLayout.vue';
import { setupNavigationGuards } from './guards';

const routes: RouteRecordRaw[] = [
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

      // 2. User Module
      {
        path: 'users',
        name: 'UserList',
        component: () => import('../modules/users/pages/UserListPage.vue'),
        meta: { title: '用户列表', parent: '用户' }
      },
      {
        path: 'users/:id',
        name: 'UserDetail',
        component: () => import('../modules/users/pages/UserDetailPage.vue'),
        meta: { title: '用户详情', parent: '用户' }
      },
      {
        path: 'devices',
        name: 'DeviceList',
        component: () => import('../modules/users/pages/DeviceListPage.vue'),
        meta: { title: '设备管理', parent: '用户' }
      },
      {
        path: 'subscriptions',
        name: 'SubscriptionList',
        component: () => import('../modules/users/pages/SubscriptionListPage.vue'),
        meta: { title: '订阅管理', parent: '用户' }
      },

      // 3. Node Module
      {
        path: 'nodes',
        name: 'NodeList',
        component: () => import('../modules/nodes/pages/NodeListPage.vue'),
        meta: { title: '节点列表', parent: '节点' }
      },
      {
        path: 'nodes/:id',
        name: 'NodeDetail',
        component: () => import('../modules/nodes/pages/NodeDetailPage.vue'),
        meta: { title: '节点详情', parent: '节点' }
      },
      {
        path: 'node-groups',
        name: 'NodeGroup',
        component: () => import('../modules/nodes/pages/NodeGroupPage.vue'),
        meta: { title: '节点组', parent: '节点' }
      },
      {
        path: 'topology',
        name: 'NodeTopology',
        component: () => import('../modules/nodes/pages/NodeTopologyPage.vue'),
        meta: { title: '节点拓扑', parent: '节点' }
      },

      // 4. Proxy Module
      {
        path: 'proxy/inbounds',
        name: 'InboundList',
        component: () => import('../modules/proxy/pages/InboundListPage.vue'),
        meta: { title: '入站管理', parent: '代理' }
      },
      {
        path: 'proxy/outbounds',
        name: 'OutboundList',
        component: () => import('../modules/proxy/pages/OutboundListPage.vue'),
        meta: { title: '出站管理', parent: '代理' }
      },
      {
        path: 'proxy/outbound-groups',
        name: 'OutboundGroup',
        component: () => import('../modules/proxy/pages/OutboundGroupPage.vue'),
        meta: { title: '出口组', parent: '代理' }
      },
      {
        path: 'proxy/chains',
        name: 'ProxyChainList',
        component: () => import('../modules/proxy/pages/ProxyChainListPage.vue'),
        meta: { title: '代理链', parent: '代理' }
      },
      {
        path: 'proxy/chains/new',
        name: 'ProxyChainCreate',
        component: () => import('../modules/proxy/pages/ProxyChainEditorPage.vue'),
        meta: { title: '新建代理链', parent: '代理' }
      },
      {
        path: 'proxy/chains/:id/edit',
        name: 'ProxyChainEdit',
        component: () => import('../modules/proxy/pages/ProxyChainEditorPage.vue'),
        meta: { title: '编辑代理链', parent: '代理' }
      },

      // 5. Routing Module
      {
        path: 'routing/rules',
        name: 'RoutingRuleList',
        component: () => import('../modules/routing/pages/RoutingRulePage.vue'),
        meta: { title: '分流规则', parent: '分流' }
      },
      {
        path: 'routing/rules/new',
        name: 'RoutingRuleCreate',
        component: () => import('../modules/routing/pages/RoutingRuleEditorPage.vue'),
        meta: { title: '新建分流规则', parent: '分流' }
      },
      {
        path: 'routing/rules/:id/edit',
        name: 'RoutingRuleEdit',
        component: () => import('../modules/routing/pages/RoutingRuleEditorPage.vue'),
        meta: { title: '编辑分流规则', parent: '分流' }
      },
      {
        path: 'routing/services',
        name: 'ServiceRule',
        component: () => import('../modules/routing/pages/ServiceRulePage.vue'),
        meta: { title: '服务规则', parent: '分流' }
      },
      {
        path: 'routing/websites',
        name: 'WebsiteControl',
        component: () => import('../modules/routing/pages/WebsiteControlPage.vue'),
        meta: { title: '网站控制', parent: '分流' }
      },
      {
        path: 'routing/simulator',
        name: 'PolicySimulator',
        component: () => import('../modules/routing/pages/PolicySimulatorPage.vue'),
        meta: { title: '策略模拟', parent: '分流' }
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
        path: 'system/admins',
        name: 'AdminList',
        component: () => import('../modules/system/pages/AdminPage.vue'),
        meta: { title: '管理员', parent: '系统' }
      },
      {
        path: 'system/agents',
        name: 'AgentList',
        component: () => import('../modules/system/pages/AgentPage.vue'),
        meta: { title: 'Agent 管理', parent: '系统' }
      },
      {
        path: 'system/cores',
        name: 'CoreVersionList',
        component: () => import('../modules/system/pages/CoreVersionPage.vue'),
        meta: { title: '核心版本', parent: '系统' }
      },
      {
        path: 'system/revisions',
        name: 'RevisionList',
        component: () => import('../modules/system/pages/RevisionPage.vue'),
        meta: { title: '配置版本', parent: '系统' }
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

router.beforeEach(setupNavigationGuards);

router.afterEach((to) => {
  const baseTitle = 'Proxy Control';
  document.title = to.meta?.title ? `${to.meta.title} - ${baseTitle}` : baseTitle;
});

