import { Routes, Route, Link } from "react-router-dom";
import VideoList from "./pages/VideoList";
import Upload from "./pages/Upload";
import VideoDetail from "./pages/VideoDetail";

function App() {
  return (
    <div className="min-h-screen bg-gray-950 text-white">
      <nav className="bg-gray-900 border-b border-gray-800 px-6 py-4">
        <div className="max-w-7xl mx-auto flex items-center justify-between">
          <Link to="/" className="text-xl font-bold text-indigo-400">
            StreamForge
          </Link>
          <div className="flex gap-4">
            <Link
              to="/"
              className="text-gray-300 hover:text-white transition-colors"
            >
              Videos
            </Link>
            <Link
              to="/upload"
              className="bg-indigo-600 hover:bg-indigo-700 text-white px-4 py-1.5 rounded-lg text-sm font-medium transition-colors"
            >
              Upload
            </Link>
          </div>
        </div>
      </nav>
      <main className="max-w-7xl mx-auto px-6 py-8">
        <Routes>
          <Route path="/" element={<VideoList />} />
          <Route path="/upload" element={<Upload />} />
          <Route path="/videos/:id" element={<VideoDetail />} />
        </Routes>
      </main>
    </div>
  );
}

export default App;
