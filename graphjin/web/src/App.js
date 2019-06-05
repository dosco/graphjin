import React, { Component } from 'react'
import { Provider } from 'react-redux'
import { Playground, store } from '@apollographql/graphql-playground-react'

import './index.css'

const fetch = window.fetch
window.fetch = function() {
  arguments[1].credentials = 'include'
  return Promise.resolve(fetch.apply(global, arguments))
}

class App extends Component {
  render() {
    return (
      <div>
        <header style={{
          background: '#4d2692', 
          color: '#f1e9ff',
          letterSpacing: '0.15rem',
          height: '65px',
          display: 'flex',
          alignItems: 'center'
          }}
        >
          <h3 style={{
            textDecoration: 'none',
            margin: '0px',
            fontSize: '18px',

            }}
          >
          <span style={{ 
            textTransform: 'uppercase',
            marginLeft: '20px'
          }}>
            Super Graph
          </span>
          <span style={{ 
            fontSize: '16px',
            marginLeft: '20px',
            color: '#b48aff'
          }}>
            Instant GraphQL</span>
          </h3>
        </header>

        <Provider store={store}>
        <Playground 
          endpoint="/api/v1/graphql"
          settings="{
            'schema.polling.enable': false,
            'request.credentials': 'include',
            'general.betaUpdates': true,
            'editor.reuseHeaders': true,
            'editor.theme': 'dark'
          }"
        />
        </Provider>
      </div>
    );
  }
}

export default App;
