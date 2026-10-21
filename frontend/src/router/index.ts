import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import CampaignDraftView from '@/views/CampaignDraftView.vue'
import ComposerView from '@/views/ComposerView.vue'

/** Campaign ids in the URL are numbers; the views get them as a number prop. */
const idProp = (route: { params: Record<string, unknown> }) => ({ id: Number(route.params.id) })

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'composer', component: ComposerView },
  { path: '/campaigns/:id(\\d+)', name: 'campaign', component: CampaignDraftView, props: idProp },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

export default router
