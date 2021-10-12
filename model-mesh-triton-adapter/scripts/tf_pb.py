# Copyright 2021 IBM Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
import os
import sys
import time
import tensorflow as tf
from tensorflow import keras

def export_h5_to_pb(path_to_h5, export_path):
    try:
        model = tf.keras.models.load_model(path_to_h5)
        os.makedirs(export_path, exist_ok = True)
        model.save(export_path)
    except (ImportError, IOError) as e:
        print('Error raised when converting keras model: \n', e, file=sys.stderr)
        exit(e.errno)

print('Converting keras model to tensorflow model. Argument(s) passed: {}'.format(str(sys.argv)))
source_path = sys.argv[1]
target_path = sys.argv[2]
start = time.time()
export_h5_to_pb(source_path, target_path)
print('Successfully converted keras model to tensorflow model. Conversion time: {} seconds'.format(time.time() - start))
os.remove(source_path)
