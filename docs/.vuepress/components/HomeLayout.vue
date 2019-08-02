<template>
  <div class="bg-white ">
  <div class="mt-16">
  <main aria-labelledby="main-title" >
    <Navbar
      @toggle-sidebar="toggleSidebar"
    />

    <div class="flex flex-col xl:flex-row border-b pb-8">
      <div class="xl:w-2/4 p-4 md:p-16 xl:pr-4">
        <div class="text-center xl:text-left">
          <h1 v-if="data.heroText !== null" class="text-5xl">{{ data.heroText || $title || 'Hello' }}</h1>
          
          <p class="text-2xl text-gray-600 leading-tight mt-4 xl:mt-0">
            {{ data.tagline || $description || 'Welcome to your VuePress site' }}
          </p>

          <NavLink
            class="inline-block px-4 py-2 my-8 bg-green-500 text-green-100 font-bold rounded"
            :item="actionLink"
          />
        </div>

        <div
          class="flex flex-wrap"
          v-if="data.features && data.features.length"
        >
          <div
            class="w-2/4"
            v-for="(feature, index) in data.features"
            :key="index"
          >
            <div class="p-4 pl-0 pb-8">
            <h2 class="md:text-2xl font-medium border-0 mb-2">{{ feature.title }}</h2>
            <p class="md:text-xl text-gray-600 leading-snug">{{ feature.details }}</p>
            </div>
          </div>
        </div>
      </div>
      
      <div class="xl:w-2/4 hidden xl:block">
       <img
          class="mt-8 border rounded shadow-xl"
          v-if="data.heroImage"
          :src="$withBase(data.heroImage)"
          :alt="data.heroAlt || 'hero'"
        >
      </div>

      <div class="block xl:hidden">
       <img
          v-if="data.heroImage"
          :src="$withBase(data.heroImageMobile)"
          :alt="data.heroAlt || 'hero'"
        >
      </div>
    </div>

    <div class="flex flex-col xl:flex-row border-b mb-8">
      <div class="xl:w-2/4 pt-8 pl-4 md:pl-16 xl:border-r">
      <h1 class="font-semibold text-2xl mb-8">Try the demo</h1>

      <pre class="text-xs md:text-lg text-black">
<span class="text-green-700"># download super graph source</span>
git clone https://github.com/dosco/super-graph.git

<span class="text-green-700"># setup the demo rails app & database and run it</span>
./demo start

<span class="text-green-700"># signin to the demo app (user1@demo.com / 123456)</span>
open http://localhost:3000

<span class="text-green-700"># try the super graph web ui</span>
open http://localhost:8080
      </pre>
      </div>

      <div class="xl:w-2/4 pt-8 pl-4 md:pl-16">
      <h1 class="font-semibold text-2xl mb-8">Try a query</h1>

      <pre class="text-xs md:text-lg text-black">
<span class="text-green-700"># query to fetch users and their products</span>
query {
  users {
    id
    email
    picture : avatar
    products(limit: 2, where: { price: { gt: 10 } }) {
      id
      name
      description
    }
  }
}
      </pre>
      </div>
    </div>

    <div
      class="mx-auto text-center py-8"
      v-if="data.footer"
    >
      {{ data.footer }}
    </div>
  </main>
  </div>
  </div>
</template>

<script>
import NavLink from '@theme/components/NavLink.vue'
import Navbar from '@theme/components/Navbar.vue'

export default {
  components: { NavLink, Navbar },

  computed: {
    data () {
      return this.$page.frontmatter
    },

    actionLink () {
      return {
        link: this.data.actionLink,
        text: this.data.actionText
      }
    }
  }
}
</script>