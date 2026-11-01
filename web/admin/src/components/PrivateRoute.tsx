import { Navigate, useLocation } from 'react-router-dom';

function PrivateRoute({ children }) {
  const location = useLocation();
  if (!localStorage.getItem('user')) {
    // `from` carries the attempted route so the login form can return the
    // visitor there. It comes from react-router's own location: the standalone
    // `history` instance in helpers never sees client-side navigations.
    return <Navigate to='/login' state={{ from: location }} replace />;
  }
  return children;
}

export { PrivateRoute };
