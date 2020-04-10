<template>
  <div>
    <main aria-labelledby="main-title" >
    <Navbar />

    <div class="container mx-auto mt-24">
      <div class="text-center">
        <div class="text-center text-4xl text-gray-800 leading-tight">
          Fetch data without code
        </div>

        <NavLink
          class="inline-block px-4 py-3 my-8 bg-blue-600 text-blue-100 font-bold rounded"
          :item="actionLink"
        />

        <a
          class="px-4 py-3 my-8 border-2 border-gray-500 text-gray-600 font-bold rounded"
          href="https://github.com/dosco/super-graph"
          target="_blank"
        >Github</a>

      </div>
    </div>

    <div class="container mx-auto mb-8">
      <div class="flex flex-wrap">
        <div class="w-100 md:w-1/2 bg-indigo-300 text-indigo-800 text-lg p-4">
          <div class="text-center text-2xl font-bold pb-2">Before, struggle with SQL</div>
          <pre>

type User struct {
  gorm.Model
  Profile   Profile
  ProfileID int
}

type Profile struct {
  gorm.Model
  Name string
}

db.Model(&user).
  Related(&profile).
  Association("Languages").
  Where("name in (?)", []string{"test"}).
  Joins("left join emails on emails.user_id = users.id")
  Find(&users)

and more ...
        </pre>
        </div>
        <div class="w-100 md:w-1/2 bg-green-300 text-black text-lg p-4">
          <div class="text-center text-2xl font-bold pb-2">With Super Graph, just ask.</div>
          <pre>
query {
  user(id: 5) {
    id
    first_name
    last_name
    picture_url
  }
  posts(first: 20, order_by: { score: desc }) {
    slug
    title
    created_at
    cached_votes_total
    vote(where: { user_id: { eq: $user_id } }) {
      id
    }
    author { id name }
    tags { id name }
  }
  posts_cursor
}
          </pre>
        </div>
      </div>
    </div>

    <div>
      <div
        class="flex flex-wrap mx-2 md:mx-20"
        v-if="data.features && data.features.length"
      >
        <div
          class="w-2/4 md:w-1/3 shadow"
          v-for="(feature, index) in data.features"
          :key="index"
        >
          <div class="p-8">
          <h2 class="md:text-xl text-blue-800 font-medium border-0 mb-1">{{ feature.title }}</h2>
          <p class="md:text-xl text-gray-700 leading-snug">{{ feature.details }}</p>
          </div>
        </div>
      </div>
    </div>


    <div class="bg-gray-100 mt-10">
      <div class="container mx-auto px-10 md:px-0 py-32">

        <div class="pb-8 hidden md:flex justify-center">
          <img src="arch-basic.svg">
        </div>

        <h1 class="uppercase font-semibold text-xl text-blue-800 text-center mb-4">
          What is Super Graph?
        </h1>
        <div class="text-2xl md:text-3xl">
          Super Graph is a library and service that fetches data from any Postgres database using just GraphQL. No more struggling with ORMs and SQL to wrangle data out of the database. No more having to figure out the right joins or making ineffiient queries. However complex the GraphQL, Super Graph will always generate just one single efficient SQL query. The goal is to save you time and money so you can focus on you're apps core value.
        </div>
      </div>
    </div>


    <div class="container mx-auto flex flex-wrap">
      <div class="md:w-1/2">
        <img src="/graphql.png">
      </div>

      <div class="md:w-1/2">
        <img src="/json.png">
      </div>
    </div>

    <div class="mt-10 py-10 md:py-20">
      <div class="container mx-auto px-10 md:px-0">
        <h1 class="uppercase font-semibold text-2xl text-blue-800 text-center">
          Try Super Graph
        </h1>

        <h1 class="uppercase font-semibold text-lg text-gray-800">
          Deploy as a service using docker
        </h1>
        <div class="bg-gray-800 text-indigo-300 p-4 rounded">
          <pre>$ git clone https://github.com/dosco/super-graph && cd super-graph && make install</pre>
          <pre>$ super-graph new blog; cd blog</pre>
          <pre>$ docker-compose run blog_api ./super-graph db:setup</pre>
          <pre>$ docker-compose up</pre>
        </div>

        <div class="border-t mt-4 pb-4"></div>

        <h1 class="uppercase font-semibold text-lg text-gray-800">
          Or use it with your own code
        </h1>
        <div class="text-md">
          <pre class="bg-gray-800 text-indigo-300 p-4 rounded">
package main

import (
  "database/sql"
  "fmt"
  "time"
  "github.com/dosco/super-graph/config"
  "github.com/dosco/super-graph/core"
  _ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
  db, err := sql.Open("pgx", "postgres://postgrs:@localhost:5432/example_db")
  if err != nil {
    log.Fatalf(err)
  }

  conf, err := config.NewConfig("./config")
  if err != nil {
    log.Fatalf(err)
  }

  sg, err = core.NewSuperGraph(conf, db)
  if err != nil {
    log.Fatalf(err)
  }

  graphqlQuery := `
    query {
      posts {
      id
      title
    }
  }`

  res, err := sg.GraphQL(context.Background(), graphqlQuery, nil)
  if err != nil {
    log.Fatalf(err)
  }

  fmt.Println(string(res.Data))
}
          </pre>
        </div>
      </div>
    </div>

    <div class="bg-gray-100 mt-10">
      <div class="container mx-auto px-10 md:px-0 py-32">
        <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
          The story of {{ data.heroText }}
        </h1>
        <div class="text-2xl md:text-3xl">
          After working on several products through my career I find that we spend way too much time on building API backends. Most APIs also require constant updating, this costs real time and money.<br><br>
          
          It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.<br><br>
          
          I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.<br><br>
          
          Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.<br><br>
          
          This compiler is what sits at the heart of Super Graph with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations and everything else needed for you to build production ready apps with it.
        </div>
      </div>
    </div>

    <div class="overflow-hidden bg-indigo-900">
      <div class="container mx-auto py-20">
      <img src="/super-graph-web-ui.png">
      </div>
    </div>

    <!--
    <div class="py-10 md:py-20">
      <div class="container mx-auto px-10 md:px-0">
        <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
          Try it with a demo Rails app
        </h1>
        <div class="text-2xl md:text-3xl">
          <small class="text-sm">Download the Docker compose config for the demo</small>
          <pre>&#8227; curl -L -o demo.yml https://bit.ly/2FZS0uw</pre>

          <small class="text-sm">Setup the demo database</small>
          <pre>&#8227; docker-compose -f demo.yml run rails_app rake db:create db:migrate db:seed</pre>

          <small class="text-sm">Run the demo</small>
          <pre>&#8227; docker-compose -f demo.yml up</pre>

          <small class="text-sm">Signin to the demo app (user1@demo.com / 123456)</small>
          <pre>&#8227; open http://localhost:3000</pre>

          <small class="text-sm">Try the super graph web ui</small>
          <pre>&#8227; open http://localhost:8080</pre>
        </div>
      </div>
    </div>
    -->

    <div class="border-t py-10">
      <div class="block md:hidden w-100">
        <iframe src='https://www.youtube.com/embed/MfPL2A-DAJk' frameborder='0' allowfullscreen style="width: 100%; height: 250px;">
        </iframe>
      </div>
      
      <div class="container mx-auto flex flex-col md:flex-row items-center">
        <div class="w-100 md:w-1/2 p-8">
          <h1 class="text-2xl font-bold">GraphQL the future of APIs</h1>
          <p class="text-xl text-gray-600">Keeping a tight and fast development loop helps you iterate quickly. Leveraging technology like Super Graph focuses your team on building the core product and not reinventing wheels. GraphQL eliminate the dependency on the backend engineering and keeps the things moving fast</p>
        </div>

        <div class="hidden md:block md:w-1/2">
          <style>.embed-container { position: relative; padding-bottom: 56.25%; height: 0; overflow: hidden; max-width: 100%; } .embed-container iframe, .embed-container object, .embed-container embed { position: absolute; top: 0; left: 0; width: 100%; height: 100%; }</style>
          <div class="embed-container shadow">
            <iframe src='https://www.youtube.com/embed/MfPL2A-DAJk' frameborder='0' allowfullscreen >
            </iframe>
          </div>
        </div>
      </div>
    </div>

    <div class="bg-gray-200 mt-10">
      <div class="container mx-auto px-10 md:px-0 py-32">
        <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
          Build Secure Apps
        </h1>
        <div class="flex flex-col text-2xl md:text-3xl">
          <card className="mb-1 p-8">
            <template #image><font-awesome-icon icon="portrait" class="text-red-500" /></template>
            <template #title>Role Based Access Control</template>
            <template #body>Dynamically assign roles like admin, manager or anon to specific users. Generate role specific queries at runtime. For example admins can get all users while others can only fetch their own user.</template>
          </card>
          <card className="mb-1 p-8">
            <template #image><font-awesome-icon icon="shield-alt" class="text-blue-500" /></template>
            <template #title>Prepared Statements</template>
            <template #body>An additional layer of protection from a variety of security issues like SQL injection. In production mode all queries are precompiled into prepared statements so only those can be executed. This also significantly speeds up all queries.</template>
          </card>
          <card className="p-8">
            <template #image><font-awesome-icon icon="lock" class="text-green-500"/></template>
            <template #title>Fuzz Tested Code</template>
            <template #body>Fuzzing is done by complex software that generates massives amounts of random input to detect if code is free of security bugs. Google uses fuzzing to protects everything from their cloud infrastructure to the Chrome browser.</template>
          </card>         
        </div>
      </div>
    </div>

    <div class="">
      <div class="container mx-auto px-10 md:px-0 py-32">
        <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
          More Features
        </h1>
        <div class="flex flex-col md:flex-row text-2xl md:text-3xl">
          <card className="mr-0 md:mr-1 mb-1 flex-col w-100 md:w-1/3 items-center">
            <!-- <template #image><img src="/arch-remote-join.svg" class="h-64"></template> -->
            <template #title>Remote Joins</template>
            <template #body>A powerful feature that allows you to query your database and remote REST APIs at the same time. For example fetch a user from the DB, his tweets from Twitter and his payments from Stripe with a single GraphQL query.</template>
          </card>
          <card className="mr-0 md:mr-1 mb-1 flex-col w-100 md:w-1/3">
            <!-- <template #image><img src="/arch-search.svg" class="h-64"></template> -->
            <template #title>Full Text Search</template>
            <template #body>Postgres has excellent full-text search built-in. You don't need another expensive service. Super Graph makes it super easy to use with keyword ranking and highlighting also supported.</template>
          </card>
          <card className="mb-1 flex-col w-100 md:w-1/3">
            <!-- <template #image><img src="/arch-bulk.svg" class="h-64"></template> -->
            <template #title>Bulk Inserts</template>
            <template #body>Efficiently insert, update and delete multiple items with a single query. Upserts are also supported</template>
          </card>         
        </div>
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
</template>

<script>
import NavLink from '@theme/components/NavLink.vue'
import Navbar from '@theme/components/Navbar.vue'
import Card from './Card.vue'


import { library } from '@fortawesome/fontawesome-svg-core'
import { faPortrait, faShieldAlt, faLock } from '@fortawesome/free-solid-svg-icons'
import { FontAwesomeIcon } from '@fortawesome/vue-fontawesome'

library.add(faPortrait, faShieldAlt, faLock)

export default {
  components: { NavLink, Navbar, FontAwesomeIcon, Card },

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