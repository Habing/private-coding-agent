import React from 'react'
import ReactDOM from 'react-dom/client'
import './index.css'

function Placeholder() {
  return (
    <div className="flex h-full items-center justify-center text-muted-foreground">
      <p>scaffolding ready — routing lands in Task 3</p>
    </div>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Placeholder />
  </React.StrictMode>,
)
