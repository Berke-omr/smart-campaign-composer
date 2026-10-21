import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import CampaignDraftView from '@/views/CampaignDraftView.vue'
import CampaignLiveView from '@/views/CampaignLiveView.vue'
import CampaignsView from '@/views/CampaignsView.vue'
import ComposerView from '@/views/ComposerView.vue'
import LiveSendsView from '@/views/LiveSendsView.vue'

/** Campaign ids in the URL are numbers; the views get them as a number prop. */
const idProp = (route: { params: Record<string, unknown> }) => ({ id: Number(route.params.id) })

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'composer', component: ComposerView },
  { path: '/campaigns', name: 'campaigns', component: CampaignsView },
  { path: '/live', name: 'live-sends', component: LiveSendsView },
  { path: '/campaigns/:id(\\d+)', name: 'campaign', component: CampaignDraftView, props: idProp },
  {
    path: '/campaigns/:id(\\d+)/live',
    name: 'campaign-live',
    component: CampaignLiveView,
    props: idProp,
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

export default router
