import { getUiMode, setUiMode } from '../../helpers';
import type { UiMode } from '../../helpers';

export const reducer = (state, action) => {
  switch (action.type) {
    case 'set':
      return {
        ...state,
        status: action.payload,
      };
    case 'unset':
      return {
        ...state,
        status: undefined,
      };
    // The interface mode is shared state rather than a value read straight from
    // storage, so switching it redraws the navigation in the same breath.
    case 'setUiMode':
      setUiMode(action.payload);
      return {
        ...state,
        uiMode: action.payload,
      };
    default:
      return state;
  }
};

export const initialState = {
  status: undefined,
  uiMode: getUiMode() as UiMode | null,
};
