// Sample JavaScript file for testing
// This file is used by extractor tests

import { something } from 'module';
import * as utils from './utils';

export function exportedFunction(x, y) {
    return x + y;
}

function privateFunction() {
    return 'private';
}

class SampleClass {
    constructor(name) {
        this.name = name;
    }

    getName() {
        return this.name;
    }

    static create(name) {
        return new SampleClass(name);
    }
}

const arrowFunc = (x) => x * 2;

const obj = {
    method() {
        return 'method';
    }
};

export { SampleClass, exportedFunction };
export default SampleClass;